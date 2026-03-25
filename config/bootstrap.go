package config

import (
	"context"

	k8s "github.com/fireflycore/go-k8s"
	microconfig "github.com/fireflycore/go-micro/config"
)

// NewStoreFromBootstrap 基于统一引导参数创建 K8s 配置存储实例。
// 流程：先按 local/remote 解析出 k8s.Conf，再创建客户端，最后构建 Store。
func NewStoreFromBootstrap(request microconfig.StoreBootstrapRequest, localLoader microconfig.LocalConfigLoader, remoteGetter microconfig.RemoteConfigGetter, payloadDecoder microconfig.PayloadDecoder, conf *Conf, opts ...microconfig.Option) (microconfig.Store, error) {
	backendConf, err := microconfig.DecodeBootstrapConfig[k8s.Conf](request, localLoader, remoteGetter, payloadDecoder)
	if err != nil {
		return nil, err
	}

	client, err := k8s.New(&backendConf)
	if err != nil {
		return nil, err
	}

	return NewStore(client, conf, opts...)
}

// LoadConfigFromStoreJSON 从 Store 读取当前配置并按 JSON 规则解码为目标类型 T。
func LoadConfigFromStoreJSON[T any](ctx context.Context, store microconfig.Store, request microconfig.StoreReadRequest, payloadDecoder microconfig.PayloadDecoder) (T, error) {
	return microconfig.DecodeStoreJSON[T](ctx, store, request, payloadDecoder)
}
