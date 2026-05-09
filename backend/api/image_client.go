package api

import (
	"context"

	"chatgpt2api/handler"
)

type imageDownloader interface {
	DownloadBytes(url string) ([]byte, error)
	DownloadAsBase64(ctx context.Context, url string) (string, error)
}

type imageWorkflowClient interface {
	handler.ImageWorkflowClient
}

type cpaRouteAwareImageWorkflowClient interface {
	imageWorkflowClient
	LastRoute() string
	LastModelLabel() string
	LastFallbackReason() string
}
