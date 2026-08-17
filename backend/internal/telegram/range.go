package telegram

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	tgdownloader "github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"tgdrive/internal/crypto"
	"tgdrive/internal/storage"
)

func (s *Store) OpenRange(ctx context.Context, file storage.File, start int64) (io.ReadCloser, error) {
	s.debugf("open range file=%s size=%d start=%d", file.Name, file.Size, start)
	if start < 0 || start > file.Size {
		return nil, fmt.Errorf("invalid file offset %d", start)
	}
	if start == 0 {
		return s.openSequential(ctx, file)
	}
	peer, ok := s.peer.Peer()
	if !ok {
		return nil, fmt.Errorf("telegram storage peer is not ready")
	}
	s.debugf("find document start message_id=%d", file.MessageID)
	doc, err := s.findDocument(ctx, peer, file.MessageID)
	if err != nil {
		s.debugf("find document failed message_id=%d err=%v", file.MessageID, err)
		return nil, err
	}
	s.debugf("find document complete message_id=%d document_id=%d dc_id=%d", file.MessageID, doc.ID, doc.DCID)
	location := &tg.InputDocumentFileLocation{ID: doc.ID, AccessHash: doc.AccessHash, FileReference: doc.FileReference}
	downloadAPI := s.downloadClientFor(ctx, doc.DCID)
	header, err := fetchFileBytes(ctx, downloadAPI, location, 0, int64(crypto.HeaderSize()), s.Debug)
	if err != nil {
		return nil, err
	}
	if string(header[:len("TGDRIVE1")]) != "TGDRIVE1" {
		return nil, crypto.ErrInvalidFormat
	}
	if header[len("TGDRIVE1")] != 2 {
		reader, err := s.openSequential(ctx, file)
		if err != nil {
			return nil, err
		}
		if _, err := io.CopyN(io.Discard, reader, start); err != nil {
			_ = reader.Close()
			return nil, err
		}
		return reader, nil
	}
	return &randomReader{ctx: ctx, api: downloadAPI, location: location, key: s.key, fileSize: file.Size, position: start, headerChecked: true, debug: s.Debug, fallback: func(offset int64) (io.ReadCloser, error) {
		reader, err := s.openSequential(ctx, file)
		if err != nil {
			return nil, err
		}
		if _, err := io.CopyN(io.Discard, reader, offset); err != nil {
			_ = reader.Close()
			return nil, err
		}
		return reader, nil
	}}, nil
}

func (s *Store) openSequential(ctx context.Context, file storage.File) (io.ReadCloser, error) {
	peer, ok := s.peer.Peer()
	if !ok {
		return nil, fmt.Errorf("telegram storage peer is not ready")
	}
	transferCtx, cancel := context.WithCancel(ctx)
	pipeReader, pipeWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	downloadDone := make(chan error, 1)
	go func() {
		s.debugf("sequential download start file=%s message_id=%d", file.Name, file.MessageID)
		doc, err := s.findDocument(transferCtx, peer, file.MessageID)
		if err == nil {
			location := &tg.InputDocumentFileLocation{ID: doc.ID, AccessHash: doc.AccessHash, FileReference: doc.FileReference}
			downloadAPI := s.downloadClientFor(transferCtx, doc.DCID)
			_, err = s.downloader.Download(downloadAPI, location).Stream(transferCtx, pipeWriter)
		}
		_ = pipeWriter.CloseWithError(err)
		s.debugf("sequential download end file=%s err=%v", file.Name, err)
		downloadDone <- err
	}()
	decryptor, err := crypto.NewDecryptor(pipeReader, s.key)
	if err != nil {
		cancel()
		_ = pipeReader.CloseWithError(err)
		_ = outputReader.Close()
		return nil, err
	}
	go func() {
		defer cancel()
		_, decryptErr := io.CopyBuffer(outputWriter, decryptor, make([]byte, crypto.ChunkSize()))
		if decryptErr != nil {
			_ = pipeReader.CloseWithError(decryptErr)
			_ = outputWriter.CloseWithError(decryptErr)
			return
		}
		downloadErr := <-downloadDone
		if downloadErr != nil {
			_ = outputWriter.CloseWithError(downloadErr)
			return
		}
		_ = outputWriter.Close()
	}()
	return &readCloser{Reader: outputReader, close: func() error {
		cancel()
		_ = pipeReader.Close()
		return outputReader.Close()
	}}, nil
}

type chunkResult struct {
	plain []byte
	err   error
}

type randomReader struct {
	ctx            context.Context
	api            tgdownloader.Client
	location       *tg.InputDocumentFileLocation
	key            string
	fileSize       int64
	position       int64
	chunk          []byte
	chunkOffset    int
	headerChecked  bool
	fallback       func(offset int64) (io.ReadCloser, error)
	fallbackReader io.ReadCloser
	debug          bool
	prefetchMu     sync.Mutex
	prefetch       chan chunkResult
	prefetchIndex  int64
	prefetchCancel context.CancelFunc
}

func (r *randomReader) Read(output []byte) (int, error) {
	if r.fallbackReader != nil {
		return r.fallbackReader.Read(output)
	}
	if r.position >= r.fileSize {
		return 0, io.EOF
	}
	written := 0
	for len(output) > 0 && r.position < r.fileSize {
		if r.chunkOffset >= len(r.chunk) {
			if err := r.loadChunk(); err != nil {
				r.stopPrefetch()
				if r.debug {
					log.Printf("telegram: random chunk failed position=%d err=%v", r.position, err)
				}
				if r.fallback != nil {
					fallback, fallbackErr := r.fallback(r.position)
					if fallbackErr == nil {
						if r.debug {
							log.Printf("telegram: random reader fallback offset=%d", r.position)
						}
						r.fallbackReader = fallback
						n, readErr := fallback.Read(output)
						return written + n, readErr
					}
					if r.debug {
						log.Printf("telegram: random fallback failed offset=%d err=%v", r.position, fallbackErr)
					}
				}
				return written, err
			}
		}
		n := copy(output, r.chunk[r.chunkOffset:])
		r.chunkOffset += n
		r.position += int64(n)
		written += n
		output = output[n:]
	}
	return written, nil
}
func (r *randomReader) Close() error {
	r.stopPrefetch()
	if r.fallbackReader != nil {
		return r.fallbackReader.Close()
	}
	return nil
}

func (r *randomReader) startPrefetch(index int64) {
	if index*int64(crypto.ChunkSize()) >= r.fileSize {
		return
	}
	r.prefetchMu.Lock()
	if r.prefetch != nil {
		r.prefetchMu.Unlock()
		return
	}
	prefetchCtx, cancel := context.WithCancel(r.ctx)
	resultCh := make(chan chunkResult, 1)
	r.prefetch = resultCh
	r.prefetchIndex = index
	r.prefetchCancel = cancel
	r.prefetchMu.Unlock()
	go func() {
		plain, err := r.fetchChunk(prefetchCtx, index)
		if r.debug {
			log.Printf("telegram: prefetch complete index=%d bytes=%d err=%v", index, len(plain), err)
		}
		resultCh <- chunkResult{plain: plain, err: err}
	}()
}

func (r *randomReader) stopPrefetch() {
	r.prefetchMu.Lock()
	cancel := r.prefetchCancel
	r.prefetchCancel = nil
	r.prefetch = nil
	r.prefetchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *randomReader) takePrefetch(index int64) ([]byte, error, bool) {
	r.prefetchMu.Lock()
	if r.prefetch == nil || r.prefetchIndex != index {
		r.prefetchMu.Unlock()
		return nil, nil, false
	}
	resultCh := r.prefetch
	r.prefetchMu.Unlock()
	result := <-resultCh
	r.prefetchMu.Lock()
	if r.prefetch == resultCh {
		r.prefetch = nil
		r.prefetchCancel = nil
	}
	r.prefetchMu.Unlock()
	return result.plain, result.err, true
}

func (r *randomReader) loadChunk() error {
	index := r.position / int64(crypto.ChunkSize())
	if plain, err, ok := r.takePrefetch(index); ok {
		if err != nil {
			return err
		}
		r.chunk, r.chunkOffset = plain, int(r.position%int64(crypto.ChunkSize()))
		r.startPrefetch(index + 1)
		return nil
	}
	plain, err := r.fetchChunk(r.ctx, index)
	if err != nil {
		return err
	}
	r.chunk, r.chunkOffset = plain, int(r.position%int64(crypto.ChunkSize()))
	r.startPrefetch(index + 1)
	return nil
}

func (r *randomReader) fetchChunk(ctx context.Context, index int64) ([]byte, error) {
	if !r.headerChecked {
		header, err := fetchFileBytes(ctx, r.api, r.location, 0, int64(crypto.HeaderSize()), r.debug)
		if err != nil {
			return nil, err
		}
		if string(header[:len("TGDRIVE1")]) != "TGDRIVE1" || header[len("TGDRIVE1")] != 2 {
			return nil, crypto.ErrInvalidFormat
		}
		r.headerChecked = true
	}
	offset := crypto.ChunkRecordOffset(index)
	if r.debug {
		log.Printf("telegram: random chunk index=%d position=%d record_offset=%d", index, index*int64(crypto.ChunkSize()), offset)
	}
	chunkStart := index * int64(crypto.ChunkSize())
	plainLength := r.fileSize - chunkStart
	if plainLength > int64(crypto.ChunkSize()) {
		plainLength = int64(crypto.ChunkSize())
	}
	if plainLength <= 0 {
		return nil, crypto.ErrInvalidFormat
	}
	var lengthHeader [4]byte
	binary.BigEndian.PutUint32(lengthHeader[:], uint32(plainLength))
	recordData, err := fetchFileBytes(ctx, r.api, r.location, offset+4, plainLength+12+16, r.debug)
	if err != nil {
		if r.debug {
			log.Printf("telegram: chunk data fetch failed offset=%d length=%d err=%v", offset+4, plainLength+28, err)
		}
		return nil, err
	}
	record := append(lengthHeader[:], recordData...)
	plain, err := crypto.DecryptChunkRecord(record, r.key)
	if err != nil {
		return nil, err
	}
	if r.debug {
		log.Printf("telegram: random chunk ready index=%d plain_length=%d", index, len(plain))
	}
	return plain, nil
}

func fetchFileBytes(ctx context.Context, api tgdownloader.Client, location tg.InputFileLocationClass, offset, length int64, debug bool) ([]byte, error) {
	const (
		alignment  = int64(4096)
		maxRequest = int64(512 * 1024)
	)
	if length <= 0 {
		return []byte{}, nil
	}

	type requestPart struct {
		resultOffset  int64
		alignedOffset int64
		skip          int64
		limit         int64
		wanted        int64
	}
	parts := make([]requestPart, 0, (length+maxRequest-1)/maxRequest)
	for planned := int64(0); planned < length; {
		current := offset + planned
		alignedOffset := current - current%alignment
		skip := current - alignedOffset
		remaining := length - planned
		limit := remaining + skip
		if limit < alignment {
			limit = alignment
		}
		limit = (limit + alignment - 1) / alignment * alignment
		if limit > maxRequest {
			limit = maxRequest
		}
		wanted := limit - skip
		if wanted > remaining {
			wanted = remaining
		}
		if wanted <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		parts = append(parts, requestPart{
			resultOffset:  planned,
			alignedOffset: alignedOffset,
			skip:          skip,
			limit:         limit,
			wanted:        wanted,
		})
		planned += wanted
	}

	result := make([]byte, length)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var waitGroup sync.WaitGroup
	var errorMu sync.Mutex
	var firstErr error
	for _, part := range parts {
		part := part
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if requestCtx.Err() != nil {
				return
			}
			if debug {
				log.Printf("telegram: get file start offset=%d limit=%d requested_offset=%d length=%d", part.alignedOffset, part.limit, offset+part.resultOffset, part.wanted)
			}
			requestStarted := time.Now()
			response, err := api.UploadGetFile(requestCtx, &tg.UploadGetFileRequest{Location: location, Offset: part.alignedOffset, Limit: int(part.limit), Precise: true})
			if err != nil {
				if debug {
					log.Printf("telegram: get file failed offset=%d limit=%d elapsed=%s err=%v", part.alignedOffset, part.limit, time.Since(requestStarted), err)
				}
				errorMu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				errorMu.Unlock()
				return
			}
			file, ok := response.(*tg.UploadFile)
			if !ok {
				err = fmt.Errorf("unsupported Telegram file response %T", response)
			} else if int64(len(file.Bytes)) < part.skip+part.wanted {
				err = io.ErrUnexpectedEOF
			} else {
				copy(result[part.resultOffset:part.resultOffset+part.wanted], file.Bytes[part.skip:part.skip+part.wanted])
			}
			if debug {
				returned := 0
				if file != nil {
					returned = len(file.Bytes)
				}
				log.Printf("telegram: get file complete offset=%d returned=%d elapsed=%s err=%v", part.alignedOffset, returned, time.Since(requestStarted), err)
			}
			if err != nil {
				errorMu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				errorMu.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}
