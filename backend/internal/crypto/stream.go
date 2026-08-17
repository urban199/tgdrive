package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	magic        = "TGDRIVE1"
	chunkSize    = 1 << 20
	headerSize   = len(magic) + 1
	nonceSize    = 12
	lengthSize   = 4
	maxChunkSize = chunkSize
)

var ErrInvalidFormat = errors.New("invalid tgdrive encrypted stream")

type Encryptor struct {
	writer      io.Writer
	gcm         cipher.AEAD
	pending     []byte
	nonce       [nonceSize]byte
	ciphertext  []byte
	wroteHeader bool
}

func NewEncryptor(writer io.Writer, apiHash string) (*Encryptor, error) {
	block, err := aes.NewCipher(keyFromAPIHash(apiHash))
	if err != nil {
		return nil, err
	}
	return &Encryptor{writer: writer, gcm: mustGCM(block)}, nil
}

func (e *Encryptor) Write(plain []byte) (int, error) {
	if err := e.writeHeader(); err != nil {
		return 0, err
	}
	written := len(plain)
	for len(plain) > 0 {
		space := chunkSize - len(e.pending)
		if space > len(plain) {
			space = len(plain)
		}
		e.pending = append(e.pending, plain[:space]...)
		plain = plain[space:]
		if len(e.pending) == chunkSize {
			if err := e.writeChunk(e.pending); err != nil {
				return written - len(plain), err
			}
			e.pending = e.pending[:0]
		}
	}
	return written, nil
}

func (e *Encryptor) Close() error {
	if err := e.writeHeader(); err != nil {
		return err
	}
	if len(e.pending) == 0 {
		return nil
	}
	if err := e.writeChunk(e.pending); err != nil {
		return err
	}
	e.pending = nil
	return nil
}

func (e *Encryptor) writeChunk(chunk []byte) error {
	if _, err := io.ReadFull(rand.Reader, e.nonce[:]); err != nil {
		return err
	}
	e.ciphertext = e.gcm.Seal(e.ciphertext[:0], e.nonce[:], chunk, nil)
	var length [lengthSize]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(chunk)))
	if err := writeFull(e.writer, length[:]); err != nil {
		return err
	}
	if err := writeFull(e.writer, e.nonce[:]); err != nil {
		return err
	}
	return writeFull(e.writer, e.ciphertext)
}

func (e *Encryptor) writeHeader() error {
	if e.wroteHeader {
		return nil
	}
	if err := writeFull(e.writer, []byte(magic)); err != nil {
		return err
	}
	if err := writeFull(e.writer, []byte{2}); err != nil {
		return err
	}
	e.wroteHeader = true
	return nil
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written < 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

type Decryptor struct {
	reader     io.Reader
	gcm        cipher.AEAD
	remaining  []byte
	nonce      [nonceSize]byte
	ciphertext []byte
	headerRead bool
	done       bool
}

func NewDecryptor(reader io.Reader, apiHash string) (*Decryptor, error) {
	block, err := aes.NewCipher(keyFromAPIHash(apiHash))
	if err != nil {
		return nil, err
	}
	return &Decryptor{reader: reader, gcm: mustGCM(block)}, nil
}

func (d *Decryptor) Read(output []byte) (int, error) {
	if !d.headerRead {
		if err := d.readHeader(); err != nil {
			return 0, err
		}
	}
	if len(d.remaining) > 0 {
		n := copy(output, d.remaining)
		d.remaining = d.remaining[n:]
		return n, nil
	}
	if d.done {
		return 0, io.EOF
	}
	var length [lengthSize]byte
	n, err := io.ReadFull(d.reader, length[:])
	if err != nil {
		if n == 0 && err == io.EOF {
			d.done = true
			return 0, io.EOF
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return 0, fmt.Errorf("truncated encrypted chunk header: %w", ErrInvalidFormat)
		}
		return 0, err
	}
	plainLength := int(binary.BigEndian.Uint32(length[:]))
	if plainLength < 0 || plainLength > maxChunkSize {
		return 0, ErrInvalidFormat
	}
	if _, err := io.ReadFull(d.reader, d.nonce[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return 0, fmt.Errorf("truncated encrypted nonce: %w", ErrInvalidFormat)
		}
		return 0, err
	}
	ciphertextLength := plainLength + d.gcm.Overhead()
	if cap(d.ciphertext) < ciphertextLength {
		d.ciphertext = make([]byte, ciphertextLength)
	} else {
		d.ciphertext = d.ciphertext[:ciphertextLength]
	}
	if _, err := io.ReadFull(d.reader, d.ciphertext); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return 0, fmt.Errorf("truncated encrypted chunk: %w", ErrInvalidFormat)
		}
		return 0, err
	}
	plain, err := d.gcm.Open(d.ciphertext[:0], d.nonce[:], d.ciphertext, nil)
	if err != nil {
		return 0, fmt.Errorf("decrypt chunk: %w", err)
	}
	d.remaining = plain
	return d.Read(output)
}

func (d *Decryptor) readHeader() error {
	var header [headerSize]byte
	if _, err := io.ReadFull(d.reader, header[:]); err != nil {
		return err
	}
	if string(header[:len(magic)]) != magic || (header[len(magic)] != 1 && header[len(magic)] != 2) {
		return ErrInvalidFormat
	}
	d.headerRead = true
	return nil
}

func keyFromAPIHash(apiHash string) []byte { sum := sha256.Sum256([]byte(apiHash)); return sum[:] }
func mustGCM(block cipher.Block) cipher.AEAD {
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return gcm
}

func DecryptChunkRecord(record []byte, apiHash string) ([]byte, error) {
	if len(record) < lengthSize+nonceSize {
		return nil, ErrInvalidFormat
	}
	plainLength := int(binary.BigEndian.Uint32(record[:lengthSize]))
	if plainLength <= 0 || plainLength > maxChunkSize || len(record) != lengthSize+nonceSize+plainLength+16 {
		return nil, ErrInvalidFormat
	}
	block, err := aes.NewCipher(keyFromAPIHash(apiHash))
	if err != nil {
		return nil, err
	}
	return mustGCM(block).Open(nil, record[lengthSize:lengthSize+nonceSize], record[lengthSize+nonceSize:], nil)
}

func EncryptString(value, apiHash string) (string, error) {
	block, err := aes.NewCipher(keyFromAPIHash(apiHash))
	if err != nil {
		return "", err
	}
	gcm := mustGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func ChunkSize() int  { return chunkSize }
func HeaderSize() int { return headerSize }
func ChunkRecordOffset(index int64) int64 {
	return int64(headerSize) + index*int64(lengthSize+nonceSize+16+chunkSize)
}
