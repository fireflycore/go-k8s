package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	microConfig "github.com/fireflycore/go-micro/config"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultNamespace = "default"
	dataKeyRaw       = "raw"
)

// StoreInstance 是基于 K8s ConfigMap 的统一配置存储实现。
type StoreInstance struct {
	// client 是外部注入的 Kubernetes 客户端。
	client kubernetes.Interface
	// options 保存通用配置参数。
	options *microConfig.Options
	// namespace 表示资源操作命名空间。
	namespace string

	// watchMu 用于保护 watchCancels 并发读写。
	watchMu sync.Mutex
	// watchCancels 保存 key 对应的取消函数。
	watchCancels map[string]context.CancelFunc
}

// NewStore 基于 Kubernetes 客户端创建配置存储实例。
func NewStore(client kubernetes.Interface, config *Config, opts ...microConfig.Option) (*StoreInstance, error) {
	// Kubernetes 客户端为空时直接报错。
	if client == nil {
		return nil, errors.New("k8s config: client is nil")
	}

	// 先构建通用 options，再保存到实例。
	var raw *microConfig.Options
	if config != nil {
		raw = config.BuildOptions(opts...)
	} else {
		raw = microConfig.NewOptions(opts...)
	}

	// 计算命名空间，优先使用 conf.Namespace。
	ns := defaultNamespace
	if config != nil && strings.TrimSpace(config.Namespace) != "" {
		ns = strings.TrimSpace(config.Namespace)
	}

	// 返回初始化完成的实例。
	return &StoreInstance{
		client:       client,
		options:      raw,
		namespace:    ns,
		watchCancels: make(map[string]context.CancelFunc),
	}, nil
}

// Get 按配置键读取当前生效配置。
func (s *StoreInstance) Get(ctx context.Context, key microConfig.Key) (*microConfig.Raw, error) {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return nil, err
	}

	// 基于 options 生成超时上下文，避免慢请求阻塞。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 读取 current ConfigMap。
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(reqCtx, s.currentName(key), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, microConfig.ErrResourceNotFound
		}
		return nil, err
	}

	// 从固定数据键提取内容。
	raw, ok := cm.Data[dataKeyRaw]
	if !ok || raw == "" {
		return nil, microConfig.ErrResourceNotFound
	}

	// 解析配置内容并返回。
	return s.decodeRaw([]byte(raw))
}

// Put 写入当前生效配置。
func (s *StoreInstance) Put(ctx context.Context, key microConfig.Key, raw *microConfig.Raw) error {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return err
	}
	// raw 为空时直接返回统一错误。
	if raw == nil {
		return microConfig.ErrInvalidRaw
	}

	// 编码配置内容。
	val, err := s.encodeRaw(raw)
	if err != nil {
		return err
	}

	// 使用超时上下文执行写入。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 确保 current ConfigMap 存在并更新数据。
	return s.upsertConfigMapData(reqCtx, s.currentName(key), map[string]string{
		dataKeyRaw: string(val),
	}, map[string]string{
		"type": "current",
	})
}

// Delete 删除当前配置。
func (s *StoreInstance) Delete(ctx context.Context, key microConfig.Key) error {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return err
	}

	// 使用超时上下文执行删除。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 删除 current ConfigMap。
	err := s.client.CoreV1().ConfigMaps(s.namespace).Delete(reqCtx, s.currentName(key), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// withTimeout 基于 options.Timeout 包装上下文。
func (s *StoreInstance) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	// 空上下文回退为 Background。
	if ctx == nil {
		ctx = context.Background()
	}
	// 无超时配置时返回可取消上下文。
	if s.options == nil || s.options.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	// 使用配置的超时时间创建上下文。
	return context.WithTimeout(ctx, s.options.Timeout)
}

// currentName 生成 current ConfigMap 名称。
func (s *StoreInstance) currentName(key microConfig.Key) string {
	return fmt.Sprintf("cfg-current-%s", shortHash(buildKeySignature(key)))
}

// buildKeySignature 构造稳定的键签名字符串。
func buildKeySignature(key microConfig.Key) string {
	tenant := strings.TrimSpace(key.TenantId)
	if tenant == "" {
		tenant = "default"
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", tenant, key.Env, key.AppId, key.Group, key.Name)
}

// shortHash 生成短哈希字符串，用于构造合法资源名。
func shortHash(raw string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(raw))
	return fmt.Sprintf("%08x", h.Sum32())
}

// upsertConfigMapData 更新或创建 ConfigMap，并合并 data。
func (s *StoreInstance) upsertConfigMapData(ctx context.Context, name string, data map[string]string, labels map[string]string) error {
	// 先读取或创建资源。
	cm, err := s.getOrCreateConfigMap(ctx, name, labels)
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	// 合并数据。
	for k, v := range data {
		cm.Data[k] = v
	}
	// 提交更新。
	_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// getOrCreateConfigMap 按名称读取 ConfigMap，不存在则创建。
func (s *StoreInstance) getOrCreateConfigMap(ctx context.Context, name string, labels map[string]string) (*corev1.ConfigMap, error) {
	// 先尝试读取已有资源。
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return cm, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	// 不存在时创建新资源。
	if labels == nil {
		labels = map[string]string{}
	}
	labels["managed-by"] = "go-k8s-config"
	created, err := s.client.CoreV1().ConfigMaps(s.namespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Data: map[string]string{},
	}, metav1.CreateOptions{})
	if err != nil {
		// 并发创建场景下，若已存在则回读。
		if apierrors.IsAlreadyExists(err) {
			return s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, name, metav1.GetOptions{})
		}
		return nil, err
	}
	return created, nil
}

// encodeRaw 对配置内容做编码。
func (s *StoreInstance) encodeRaw(raw *microConfig.Raw) ([]byte, error) {
	// 优先使用调用方注入的编解码器。
	if s.options != nil && s.options.Codec != nil {
		return s.options.Codec.Marshal(raw)
	}
	// 默认使用 JSON 编码。
	return json.Marshal(raw)
}

// decodeRaw 对配置内容做解码。
func (s *StoreInstance) decodeRaw(data []byte) (*microConfig.Raw, error) {
	// 准备承载结果对象。
	raw := new(microConfig.Raw)
	// 优先使用调用方注入的编解码器。
	if s.options != nil && s.options.Codec != nil {
		if err := s.options.Codec.Unmarshal(data, raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	// 默认使用 JSON 解码。
	if err := json.Unmarshal(data, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// validateKey 校验配置键合法性。
func validateKey(key microConfig.Key) error {
	// Env 为空时视为无效 key。
	if strings.TrimSpace(key.Env) == "" {
		return microConfig.ErrInvalidKey
	}
	// AppId 为空时视为无效 key。
	if strings.TrimSpace(key.AppId) == "" {
		return microConfig.ErrInvalidKey
	}
	// Group 为空时视为无效 key。
	if strings.TrimSpace(key.Group) == "" {
		return microConfig.ErrInvalidKey
	}
	// Name 为空时视为无效 key。
	if strings.TrimSpace(key.Name) == "" {
		return microConfig.ErrInvalidKey
	}
	// key 校验通过。
	return nil
}
