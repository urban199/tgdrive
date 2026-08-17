package telegram

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"tgdrive/internal/crypto"
)

const indexBackupName = "tgdrive-index-v1.enc"

func (s *Store) BackupIndex(ctx context.Context, snapshot []byte) error {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	s.backupQueueMu.Lock()
	s.backupLastAt = time.Now()
	s.backupQueueMu.Unlock()
	peer, ok := s.peer.Peer()
	if !ok {
		return fmt.Errorf("telegram storage peer is not ready")
	}
	previousID := s.backupMessageID
	if previousID == 0 {
		previousID, _ = s.findPinnedBackup(ctx, peer)
	}
	pipeReader, pipeWriter := io.Pipe()
	copyDone := make(chan error, 1)
	go func() {
		encryptor, err := crypto.NewEncryptor(pipeWriter, s.key)
		if err == nil {
			_, err = encryptor.Write(snapshot)
		}
		if err == nil {
			err = encryptor.Close()
		}
		_ = pipeWriter.CloseWithError(err)
		copyDone <- err
	}()
	inputFile, err := s.uploader.Upload(ctx, uploader.NewUpload(indexBackupName, pipeReader, -1))
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return err
	}
	if err := <-copyDone; err != nil {
		return err
	}
	backupMedia := &tg.InputMediaUploadedDocument{
		File:      inputFile,
		ForceFile: true,
		MimeType:  "application/octet-stream",
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeFilename{FileName: indexBackupName},
		},
	}
	messageID := previousID
	if previousID != 0 {
		_, err = limitedRPC(ctx, s.messageLimiter, func() (tg.UpdatesClass, error) {
			return s.api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
				Peer:    peer,
				ID:      previousID,
				Message: "tgdrive-index-v1",
				Media:   backupMedia,
			})
		})
		if err != nil {
			return fmt.Errorf("edit pinned index backup: %w", err)
		}
	} else {
		result, sendErr := limitedRPC(ctx, s.messageLimiter, func() (tg.UpdatesClass, error) {
			return s.api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
				Peer:     peer,
				Media:    backupMedia,
				Message:  "tgdrive-index-v1",
				RandomID: randomID(),
			})
		})
		if sendErr != nil {
			return sendErr
		}
		messageID, err = messageIDFromUpdates(result)
		if err != nil {
			return err
		}
		if err := s.pinBackup(ctx, peer, messageID); err != nil {
			return err
		}
	}
	s.backupMessageID = messageID
	return nil
}

func (s *Store) RestoreIndex(ctx context.Context) ([]byte, bool, error) {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	peer, ok := s.peer.Peer()
	if !ok {
		return nil, false, fmt.Errorf("telegram storage peer is not ready")
	}
	messageID := s.backupMessageID
	if messageID == 0 {
		var err error
		messageID, err = s.findPinnedBackup(ctx, peer)
		if err != nil {
			return nil, false, err
		}
	}
	if messageID == 0 {
		return nil, false, nil
	}
	doc, err := s.findDocument(ctx, peer, messageID)
	if err != nil {
		return nil, false, err
	}
	location := &tg.InputDocumentFileLocation{ID: doc.ID, AccessHash: doc.AccessHash, FileReference: doc.FileReference}
	pipeReader, pipeWriter := io.Pipe()
	downloadAPI := s.downloadClientFor(ctx, doc.DCID)
	downloadDone := make(chan error, 1)
	go func() {
		_, err := s.downloader.Download(downloadAPI, location).Stream(ctx, pipeWriter)
		_ = pipeWriter.CloseWithError(err)
		downloadDone <- err
	}()
	decryptor, err := crypto.NewDecryptor(pipeReader, s.key)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return nil, false, err
	}
	content, decryptErr := io.ReadAll(decryptor)
	if decryptErr == nil {
		decryptErr = <-downloadDone
	}
	if decryptErr != nil {
		return nil, false, decryptErr
	}
	s.backupMessageID = messageID
	return content, true, nil
}

func (s *Store) findPinnedBackup(ctx context.Context, peer tg.InputPeerClass) (int, error) {
	pinnedID := 0
	switch value := peer.(type) {
	case *tg.InputPeerChannel:
		full, err := limitedRPC(ctx, s.messageLimiter, func() (*tg.MessagesChatFull, error) {
			return s.api.ChannelsGetFullChannel(ctx, &tg.InputChannel{ChannelID: value.ChannelID, AccessHash: value.AccessHash})
		})
		if err != nil {
			return 0, err
		}
		if channel, ok := full.FullChat.(*tg.ChannelFull); ok {
			pinnedID = channel.PinnedMsgID
		}
	case *tg.InputPeerChat:
		full, err := limitedRPC(ctx, s.messageLimiter, func() (*tg.MessagesChatFull, error) {
			return s.api.MessagesGetFullChat(ctx, value.ChatID)
		})
		if err != nil {
			return 0, err
		}
		if chat, ok := full.FullChat.(*tg.ChatFull); ok {
			pinnedID = chat.PinnedMsgID
		}
	default:
		return 0, nil
	}
	if pinnedID == 0 {
		return 0, nil
	}
	doc, err := s.findDocument(ctx, peer, pinnedID)
	if err != nil || documentFilename(doc) != indexBackupName {
		return 0, nil
	}
	return pinnedID, nil
}

func (s *Store) pinBackup(ctx context.Context, peer tg.InputPeerClass, messageID int) error {
	_, err := limitedRPC(ctx, s.messageLimiter, func() (tg.UpdatesClass, error) {
		return s.api.MessagesUpdatePinnedMessage(ctx, &tg.MessagesUpdatePinnedMessageRequest{Peer: peer, ID: messageID, Silent: true})
	})
	return err
}

func documentFilename(document *tg.Document) string {
	if document == nil {
		return ""
	}
	for _, attribute := range document.Attributes {
		if filename, ok := attribute.(*tg.DocumentAttributeFilename); ok {
			return filename.FileName
		}
	}
	return ""
}
