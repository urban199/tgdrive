package telegram

import (
	"context"
	"time"

	tgdownloader "github.com/gotd/td/telegram/downloader"
	tguploader "github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type rpcLimiter struct {
	slots chan struct{}
}

func newRPCLimiter(maxConcurrent int) *rpcLimiter {
	return &rpcLimiter{slots: make(chan struct{}, maxConcurrent)}
}

func (l *rpcLimiter) acquire(ctx context.Context) error {
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *rpcLimiter) release() {
	<-l.slots
}

func floodWaitDuration(err error) (time.Duration, bool) {
	rpcErr, ok := tgerr.As(err)
	if !ok || !rpcErr.IsOneOf("FLOOD_WAIT", "FLOOD_PREMIUM_WAIT") {
		return 0, false
	}
	wait := time.Duration(rpcErr.Argument) * time.Second
	if wait < time.Second {
		wait = time.Second
	}
	return wait + time.Second, true
}

// limitedRPC retries indefinitely only when Telegram explicitly returns FLOOD_WAIT.
// The wait duration comes from Telegram, with one second added as a safety margin.
func limitedRPC[T any](ctx context.Context, limiter *rpcLimiter, call func() (T, error)) (T, error) {
	for {
		if err := limiter.acquire(ctx); err != nil {
			var zero T
			return zero, err
		}
		result, err := call()
		limiter.release()
		if err == nil {
			return result, nil
		}
		wait, ok := floodWaitDuration(err)
		if !ok {
			return result, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return result, ctx.Err()
		}
	}
}

type rateLimitedUploadClient struct {
	client  tguploader.Client
	limiter *rpcLimiter
}

func (c rateLimitedUploadClient) UploadSaveFilePart(ctx context.Context, request *tg.UploadSaveFilePartRequest) (bool, error) {
	return limitedRPC(ctx, c.limiter, func() (bool, error) {
		return c.client.UploadSaveFilePart(ctx, request)
	})
}

func (c rateLimitedUploadClient) UploadSaveBigFilePart(ctx context.Context, request *tg.UploadSaveBigFilePartRequest) (bool, error) {
	return limitedRPC(ctx, c.limiter, func() (bool, error) {
		return c.client.UploadSaveBigFilePart(ctx, request)
	})
}

type rateLimitedDownloadClient struct {
	client  tgdownloader.Client
	limiter *rpcLimiter
}

func (c rateLimitedDownloadClient) UploadGetFile(ctx context.Context, request *tg.UploadGetFileRequest) (tg.UploadFileClass, error) {
	return limitedRPC(ctx, c.limiter, func() (tg.UploadFileClass, error) {
		return c.client.UploadGetFile(ctx, request)
	})
}

func (c rateLimitedDownloadClient) UploadGetFileHashes(ctx context.Context, request *tg.UploadGetFileHashesRequest) ([]tg.FileHash, error) {
	return limitedRPC(ctx, c.limiter, func() ([]tg.FileHash, error) {
		return c.client.UploadGetFileHashes(ctx, request)
	})
}

func (c rateLimitedDownloadClient) UploadReuploadCDNFile(ctx context.Context, request *tg.UploadReuploadCDNFileRequest) ([]tg.FileHash, error) {
	return limitedRPC(ctx, c.limiter, func() ([]tg.FileHash, error) {
		return c.client.UploadReuploadCDNFile(ctx, request)
	})
}

func (c rateLimitedDownloadClient) UploadGetCDNFileHashes(ctx context.Context, request *tg.UploadGetCDNFileHashesRequest) ([]tg.FileHash, error) {
	return limitedRPC(ctx, c.limiter, func() ([]tg.FileHash, error) {
		return c.client.UploadGetCDNFileHashes(ctx, request)
	})
}

func (c rateLimitedDownloadClient) UploadGetWebFile(ctx context.Context, request *tg.UploadGetWebFileRequest) (*tg.UploadWebFile, error) {
	return limitedRPC(ctx, c.limiter, func() (*tg.UploadWebFile, error) {
		return c.client.UploadGetWebFile(ctx, request)
	})
}
