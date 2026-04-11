package config

import (
	"context"

	k8s "github.com/fireflycore/go-k8s"
	microConfig "github.com/fireflycore/go-micro/config"
)

// NewStoreFromLoader 基于统一加载参数创建 K8s 配置存储实例。
// 流程：先按 local / remote 解析出 k8s.Conf，再创建客户端，最后构建 Store。
func NewStoreFromLoader(params microConfig.LoaderParams, localLoad microConfig.LocalLoaderFunc, remoteLoad microConfig.RemoteLoaderFunc, payloadDecode microConfig.PayloadDecodeFunc, conf *Config, opts ...microConfig.Option) (microConfig.Store, error) {
	backendConf, err := microConfig.LoadConfig[k8s.Conf](params, localLoad, remoteLoad, payloadDecode)
	if err != nil {
		return nil, err
	}

	client, err := k8s.New(&backendConf)
	if err != nil {
		return nil, err
	}

	return NewStore(client, conf, opts...)
}

// LoadConfigFromStore 从 Store 读取当前配置并解码为目标类型 T。
// 当 Raw.Encrypted=true 时，会复用 go-micro/config 的统一规则，先解密整份内容，再解析目标结构。
func LoadConfigFromStore[T any](ctx context.Context, store microConfig.Store, params microConfig.StoreParams, payloadDecode microConfig.PayloadDecodeFunc) (T, error) {
	return microConfig.LoadStoreConfig[T](ctx, store, params, payloadDecode)
}
