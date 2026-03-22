package server

import (
	"context"
	"time"

	grpc "google.golang.org/grpc"
)

type (
	downloadMetaKey      struct{}
	instanceStartMetaKey struct{}

	downloadMetaHolder struct {
		Meta *DownloadMeta
	}

	instanceStartMetaHolder struct {
		Meta *InstanceStartMeta
	}
)

type DownloadMeta struct {
	FunctionID     int64
	Downloaded     bool
	DownloadPath   string
	StoredPath     string
	BytesWritten   int64
	DownloadTook   time.Duration
	ReusedFromFS   bool
	ReusedInFlight bool
}

type InstanceStartMeta struct {
	FunctionPath   string
	InstanceID     string
	RuntimeType    string
	InstanceAddr   string
	StartTook      time.Duration
	ReusedAssigned bool
}

func WithDownloadMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &downloadMetaHolder{}
	ctx = context.WithValue(ctx, downloadMetaKey{}, holder)
	return handler(ctx, req)
}

func WithInstanceStartMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &instanceStartMetaHolder{}
	ctx = context.WithValue(ctx, instanceStartMetaKey{}, holder)
	return handler(ctx, req)
}

func GetDownloadMeta(ctx context.Context) *DownloadMeta {
	holder, ok := ctx.Value(downloadMetaKey{}).(*downloadMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetDownloadMeta(ctx context.Context, meta *DownloadMeta) {
	holder, ok := ctx.Value(downloadMetaKey{}).(*downloadMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}

func GetInstanceStartMeta(ctx context.Context) *InstanceStartMeta {
	holder, ok := ctx.Value(instanceStartMetaKey{}).(*instanceStartMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetInstanceStartMeta(ctx context.Context, meta *InstanceStartMeta) {
	holder, ok := ctx.Value(instanceStartMetaKey{}).(*instanceStartMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}
