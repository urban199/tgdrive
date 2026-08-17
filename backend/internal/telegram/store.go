package telegram

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"tgdrive/internal/crypto"
	"tgdrive/internal/storage"
)

const (
	// MaxFileSize is Telegram's maximum encrypted media payload size.
	MaxFileSize         int64 = 2 * 1024 * 1024 * 1024
	indexBackupInterval       = 5 * time.Second
)

type PeerResolver interface {
	Peer() (tg.InputPeerClass, bool)
}

type MediaAPIProvider func(ctx context.Context, dcID int) (*tg.Client, io.Closer, error)

type Store struct {
	api             *tg.Client
	downloadAPI     *tg.Client
	mediaProvider   MediaAPIProvider
	mediaMu         sync.Mutex
	mediaAPIs       map[int]*tg.Client
	mediaClosers    []io.Closer
	peer            PeerResolver
	key             string
	uploadLimiter   *rpcLimiter
	downloadLimiter *rpcLimiter
	messageLimiter  *rpcLimiter
	uploader        *uploader.Uploader
	downloader      *downloader.Downloader
	backupMu        sync.Mutex
	backupMessageID int
	backupQueueMu   sync.Mutex
	backupPending   []byte
	backupTimer     *time.Timer
	backupLastAt    time.Time
	backupWG        sync.WaitGroup
	Debug           bool
}

func NewStore(api *tg.Client, peer PeerResolver, key string) *Store {
	uploadLimiter := newRPCLimiter(2)
	downloadLimiter := newRPCLimiter(4)
	messageLimiter := newRPCLimiter(1)
	return &Store{
		api:             api,
		downloadAPI:     api,
		peer:            peer,
		key:             key,
		mediaAPIs:       make(map[int]*tg.Client),
		uploadLimiter:   uploadLimiter,
		downloadLimiter: downloadLimiter,
		messageLimiter:  messageLimiter,
		uploader:        uploader.NewUploader(rateLimitedUploadClient{client: api, limiter: uploadLimiter}).WithThreads(2),
		downloader:      downloader.NewDownloader(),
	}
}

func (s *Store) SetDownloadAPI(api *tg.Client) {
	if api != nil {
		s.downloadAPI = api
	}
}

func (s *Store) QueueIndexBackup(snapshot []byte) {
	pending := append([]byte(nil), snapshot...)
	s.backupQueueMu.Lock()
	s.backupPending = pending
	if s.backupTimer == nil {
		delay := indexBackupInterval
		if since := time.Since(s.backupLastAt); since >= indexBackupInterval {
			delay = 0
		} else if s.backupLastAt.IsZero() {
			delay = 0
		} else {
			delay -= since
		}
		s.backupTimer = time.AfterFunc(delay, s.flushQueuedBackup)
	}
	s.backupQueueMu.Unlock()
}

func (s *Store) flushQueuedBackup() {
	s.backupQueueMu.Lock()
	snapshot := s.backupPending
	s.backupPending = nil
	s.backupTimer = nil
	if len(snapshot) != 0 {
		s.backupWG.Add(1)
	}
	s.backupQueueMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	defer s.backupWG.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.BackupIndex(ctx, snapshot); err != nil {
		s.debugf("queued index backup failed: %v", err)
	}
}

func (s *Store) flushPendingBackup() {
	s.backupQueueMu.Lock()
	if s.backupTimer != nil {
		s.backupTimer.Stop()
		s.backupTimer = nil
	}
	snapshot := s.backupPending
	s.backupPending = nil
	s.backupQueueMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.BackupIndex(ctx, snapshot); err != nil {
		s.debugf("final index backup failed: %v", err)
	}
}

func (s *Store) SetMediaAPIProvider(provider MediaAPIProvider) {
	s.mediaMu.Lock()
	s.mediaProvider = provider
	s.mediaMu.Unlock()
}

func (s *Store) downloadAPIFor(ctx context.Context, dcID int) *tg.Client {
	if dcID <= 0 {
		return s.downloadAPI
	}
	s.mediaMu.Lock()
	if api := s.mediaAPIs[dcID]; api != nil {
		s.mediaMu.Unlock()
		return api
	}
	provider := s.mediaProvider
	s.mediaMu.Unlock()
	if provider == nil {
		return s.downloadAPI
	}
	api, closer, err := provider(ctx, dcID)
	if err != nil {
		s.debugf("media pool unavailable dc=%d, using main connection: %v", dcID, err)
		return s.downloadAPI
	}
	s.mediaMu.Lock()
	if existing := s.mediaAPIs[dcID]; existing != nil {
		s.mediaMu.Unlock()
		if closer != nil {
			_ = closer.Close()
		}
		return existing
	}
	s.mediaAPIs[dcID] = api
	if closer != nil {
		s.mediaClosers = append(s.mediaClosers, closer)
	}
	s.mediaMu.Unlock()
	s.debugf("media pool ready dc=%d", dcID)
	return api
}

func (s *Store) downloadClientFor(ctx context.Context, dcID int) downloader.Client {
	return rateLimitedDownloadClient{client: s.downloadAPIFor(ctx, dcID), limiter: s.downloadLimiter}
}

func (s *Store) Close() error {
	s.backupQueueMu.Lock()
	if s.backupTimer != nil {
		s.backupTimer.Stop()
		s.backupTimer = nil
	}
	s.backupQueueMu.Unlock()
	s.backupWG.Wait()
	s.flushPendingBackup()

	s.mediaMu.Lock()
	closers := s.mediaClosers
	s.mediaClosers = nil
	s.mediaMu.Unlock()
	var closeErr error
	for _, closer := range closers {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (s *Store) debugf(format string, args ...any) {
	if s.Debug {
		log.Printf("telegram: "+format, args...)
	}
}

func (s *Store) Put(ctx context.Context, fileName string, input io.Reader, size int64) (storage.File, error) {
	return s.put(ctx, fileName, input, size, nil)
}

func (s *Store) PutWithProgress(
	ctx context.Context,
	fileName string,
	input io.Reader,
	size int64,
	onProgress func(uploadedBytes, totalBytes int64) error,
) (storage.File, error) {
	return s.put(ctx, fileName, input, size, onProgress)
}

func (s *Store) put(
	ctx context.Context,
	fileName string,
	input io.Reader,
	size int64,
	onProgress func(uploadedBytes, totalBytes int64) error,
) (storage.File, error) {
	if size >= 0 && encryptedSize(size) > MaxFileSize {
		return storage.File{}, fmt.Errorf(
			"file is %d bytes after encryption; Telegram allows at most %d bytes",
			encryptedSize(size),
			MaxFileSize,
		)
	}
	peer, ok := s.peer.Peer()
	if !ok {
		return storage.File{}, fmt.Errorf("telegram storage peer is not ready")
	}
	remoteName, err := encryptedRemoteName(fileName, s.key)
	if err != nil {
		return storage.File{}, err
	}
	telegramTotal := int64(-1)
	if size >= 0 {
		telegramTotal = encryptedSize(size)
	}
	if onProgress != nil {
		if err := onProgress(0, telegramTotal); err != nil {
			return storage.File{}, err
		}
	}
	pipeReader, pipeWriter := io.Pipe()
	counting := &countingReader{reader: input, max: maxPlainFileSize()}
	encryptor, err := crypto.NewEncryptor(pipeWriter, s.key)
	if err != nil {
		return storage.File{}, err
	}
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.CopyBuffer(encryptor, counting, make([]byte, crypto.ChunkSize()))
		if copyErr == nil {
			copyErr = encryptor.Close()
		}
		_ = pipeWriter.CloseWithError(copyErr)
		copyDone <- copyErr
	}()

	telegramUploader := s.uploader
	if onProgress != nil {
		telegramUploader = uploader.NewUploader(
			rateLimitedUploadClient{client: s.api, limiter: s.uploadLimiter},
		).WithThreads(2).WithProgress(uploadProgressFunc{callback: onProgress})
	}
	inputFile, err := telegramUploader.Upload(ctx, uploader.NewUpload(remoteName, pipeReader, telegramTotal))
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		<-copyDone
		return storage.File{}, err
	}
	if err := <-copyDone; err != nil {
		return storage.File{}, err
	}
	result, err := limitedRPC(ctx, s.messageLimiter, func() (tg.UpdatesClass, error) {
		return s.api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{Peer: peer, Media: &tg.InputMediaUploadedDocument{File: inputFile, ForceFile: true, MimeType: "application/octet-stream", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: remoteName}}}, RandomID: randomID()})
	})
	if err != nil {
		return storage.File{}, err
	}
	messageID, err := messageIDFromUpdates(result)
	if err != nil {
		return storage.File{}, err
	}
	return storage.File{Name: fileName, Size: counting.total, EncryptedSize: encryptedSize(counting.total), MessageID: messageID, UpdatedAt: now()}, nil
}

type uploadProgressFunc struct {
	callback func(uploadedBytes, totalBytes int64) error
}

func (p uploadProgressFunc) Chunk(ctx context.Context, state uploader.ProgressState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.callback(state.Uploaded, state.Total)
}

func (s *Store) Open(ctx context.Context, file storage.File) (io.ReadCloser, error) {
	return s.OpenRange(ctx, file, 0)
}

func (s *Store) Delete(ctx context.Context, file storage.File) error {
	peer, ok := s.peer.Peer()
	if !ok {
		return fmt.Errorf("telegram storage chat is not ready")
	}
	switch value := peer.(type) {
	case *tg.InputPeerChannel:
		_, err := limitedRPC(ctx, s.messageLimiter, func() (*tg.MessagesAffectedMessages, error) {
			return s.api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{Channel: &tg.InputChannel{ChannelID: value.ChannelID, AccessHash: value.AccessHash}, ID: []int{file.MessageID}})
		})
		return err
	default:
		_, err := limitedRPC(ctx, s.messageLimiter, func() (*tg.MessagesAffectedMessages, error) {
			return s.api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{ID: []int{file.MessageID}, Revoke: true})
		})
		return err
	}
}

type readCloser struct {
	io.Reader
	close func() error
}

func (r *readCloser) Close() error { return r.close() }

type countingReader struct {
	reader io.Reader
	total  int64
	max    int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.total == r.max {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n > 0 {
			return 0, fmt.Errorf(
				"file exceeds the maximum plaintext size of %d bytes; encryption would exceed Telegram's %d-byte limit",
				r.max,
				MaxFileSize,
			)
		}
		return 0, err
	}
	if int64(len(p)) > r.max-r.total {
		p = p[:int(r.max-r.total)]
	}
	n, err := r.reader.Read(p)
	r.total += int64(n)
	return n, err
}
func encryptedRemoteName(originalName, key string) (string, error) {
	encoded, err := crypto.EncryptString(originalName, key)
	if err != nil {
		return "", err
	}
	return "tg-" + encoded + ".bin", nil
}
func encryptedSize(size int64) int64 {
	chunks := (size + int64(crypto.ChunkSize()) - 1) / int64(crypto.ChunkSize())
	if size == 0 {
		chunks = 0
	}
	return int64(9) + size + chunks*32
}

func maxPlainFileSize() int64 {
	minimum, maximum := int64(0), MaxFileSize
	for minimum < maximum {
		candidate := minimum + (maximum-minimum+1)/2
		if encryptedSize(candidate) <= MaxFileSize {
			minimum = candidate
		} else {
			maximum = candidate - 1
		}
	}
	return minimum
}
func randomID() int64 { return int64(timeNow().UnixNano()) }

var timeNow = func() time.Time { return time.Now() }

func now() time.Time { return timeNow() }

func messageIDFromUpdates(updates tg.UpdatesClass) (int, error) {
	switch result := updates.(type) {
	case *tg.Updates:
		for _, update := range result.Updates {
			if id, ok := messageIDFromUpdate(update); ok {
				return id, nil
			}
		}
	case *tg.UpdatesCombined:
		for _, update := range result.Updates {
			if id, ok := messageIDFromUpdate(update); ok {
				return id, nil
			}
		}
	case *tg.UpdateShortSentMessage:
		return result.ID, nil
	}
	return 0, fmt.Errorf("telegram did not return a message id")
}

func messageIDFromUpdate(update tg.UpdateClass) (int, bool) {
	switch value := update.(type) {
	case *tg.UpdateNewMessage:
		message, ok := value.Message.(*tg.Message)
		if ok {
			return message.ID, true
		}
	case *tg.UpdateNewChannelMessage:
		message, ok := value.Message.(*tg.Message)
		if ok {
			return message.ID, true
		}
	case *tg.UpdateNewScheduledMessage:
		message, ok := value.Message.(*tg.Message)
		if ok {
			return message.ID, true
		}
	}
	return 0, false
}

func (s *Store) findDocument(ctx context.Context, peer tg.InputPeerClass, messageID int) (*tg.Document, error) {
	var messages tg.MessagesMessagesClass
	var err error
	switch value := peer.(type) {
	case *tg.InputPeerChannel:
		messages, err = limitedRPC(ctx, s.messageLimiter, func() (tg.MessagesMessagesClass, error) {
			return s.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: value.ChannelID, AccessHash: value.AccessHash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}}})
		})
	default:
		messages, err = limitedRPC(ctx, s.messageLimiter, func() (tg.MessagesMessagesClass, error) {
			return s.api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}})
		})
	}
	if err != nil {
		return nil, err
	}
	switch result := messages.(type) {
	case *tg.MessagesChannelMessages:
		for _, message := range result.Messages {
			if doc := documentFromMessage(message); doc != nil {
				return doc, nil
			}
		}
	case *tg.MessagesMessages:
		for _, message := range result.Messages {
			if doc := documentFromMessage(message); doc != nil {
				return doc, nil
			}
		}
	case *tg.MessagesMessagesSlice:
		for _, message := range result.Messages {
			if doc := documentFromMessage(message); doc != nil {
				return doc, nil
			}
		}
	}
	return nil, fmt.Errorf("telegram document %d not found", messageID)
}

func documentFromMessage(message tg.MessageClass) *tg.Document {
	m, ok := message.(*tg.Message)
	if !ok {
		return nil
	}
	media, ok := m.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil
	}
	doc, ok := media.Document.(*tg.Document)
	if !ok {
		return nil
	}
	return doc
}
