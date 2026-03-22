package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"

	microconfig "github.com/fireflycore/go-micro/config"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultNamespace = "default"
	dataKeyItem      = "item"
	dataKeyMeta      = "meta"
)

// StoreInstance 是基于 K8s ConfigMap 的统一配置存储实现。
type StoreInstance struct {
	// client 是外部注入的 Kubernetes 客户端。
	client kubernetes.Interface
	// options 保存通用配置参数。
	options *microconfig.Options
	// namespace 表示资源操作命名空间。
	namespace string

	// watchMu 用于保护 watchCancels 并发读写。
	watchMu sync.Mutex
	// watchCancels 保存 key 对应的取消函数。
	watchCancels map[string]context.CancelFunc
}

// NewStore 基于 Kubernetes 客户端创建配置存储实例。
func NewStore(client kubernetes.Interface, conf *Conf, opts ...microconfig.Option) (*StoreInstance, error) {
	// Kubernetes 客户端为空时直接报错。
	if client == nil {
		return nil, errors.New("k8s config: client is nil")
	}

	// 先构建通用 options，再保存到实例。
	var raw *microconfig.Options
	if conf != nil {
		raw = conf.BuildOptions(opts...)
	} else {
		raw = microconfig.NewOptions(opts...)
	}

	// 计算命名空间，优先使用 conf.Namespace。
	ns := defaultNamespace
	if conf != nil && strings.TrimSpace(conf.Namespace) != "" {
		ns = strings.TrimSpace(conf.Namespace)
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
func (s *StoreInstance) Get(ctx context.Context, key microconfig.Key) (*microconfig.Item, error) {
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
			return nil, microconfig.ErrResourceNotFound
		}
		return nil, err
	}

	// 从固定数据键提取内容。
	raw, ok := cm.Data[dataKeyItem]
	if !ok || raw == "" {
		return nil, microconfig.ErrResourceNotFound
	}

	// 解析配置内容并返回。
	return s.decodeItem([]byte(raw))
}

// GetByQuery 按运行时上下文读取配置。
func (s *StoreInstance) GetByQuery(ctx context.Context, query microconfig.Query) (*microconfig.Item, error) {
	// 复制基础 key，避免修改入参。
	key := query.Key
	// 若 key 未携带租户，则回退到 query.TenantID。
	if key.Tenant == "" {
		key.Tenant = query.TenantID
	}
	// 若 key 未携带 appId，则回退到 query.AppID。
	if key.AppID == "" {
		key.AppID = query.AppID
	}
	// 复用 Get 逻辑，保持行为一致。
	return s.Get(ctx, key)
}

// Put 写入当前生效配置。
func (s *StoreInstance) Put(ctx context.Context, key microconfig.Key, item *microconfig.Item) error {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return err
	}
	// item 为空时直接返回统一错误。
	if item == nil {
		return microconfig.ErrInvalidItem
	}

	// 编码配置内容。
	val, err := s.encodeItem(item)
	if err != nil {
		return err
	}

	// 使用超时上下文执行写入。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 确保 current ConfigMap 存在并更新数据。
	return s.upsertConfigMapData(reqCtx, s.currentName(key), map[string]string{
		dataKeyItem: string(val),
	}, map[string]string{
		"type": "current",
	})
}

// Delete 删除当前配置。
func (s *StoreInstance) Delete(ctx context.Context, key microconfig.Key) error {
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

// PutVersion 写入版本快照并返回版本号。
func (s *StoreInstance) PutVersion(ctx context.Context, key microconfig.Key, item *microconfig.Item) (string, error) {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return "", err
	}
	// item 为空时直接返回统一错误。
	if item == nil {
		return "", microconfig.ErrInvalidItem
	}

	// 若调用方未显式提供版本号，则按 key hash 版本号策略外部生成。
	version := item.Version
	if version == "" {
		return "", microconfig.ErrInvalidItem
	}

	// 构造写入版本快照的数据副本。
	versioned := *item
	versioned.Version = version

	// 编码版本内容。
	val, err := s.encodeItem(&versioned)
	if err != nil {
		return "", err
	}

	// 使用超时上下文执行写入。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 先读取或创建 versions ConfigMap。
	name := s.versionsName(key)
	cm, err := s.getOrCreateConfigMap(reqCtx, name, map[string]string{"type": "versions"})
	if err != nil {
		return "", err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}

	// 版本冲突检查：同版本已存在且内容不同。
	if prev, ok := cm.Data[version]; ok && prev != string(val) {
		return "", microconfig.ErrVersionConflict
	}
	cm.Data[version] = string(val)

	// 提交更新。
	if _, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(reqCtx, cm, metav1.UpdateOptions{}); err != nil {
		return "", err
	}

	// 返回最终版本号。
	return version, nil
}

// GetVersion 读取指定版本快照。
func (s *StoreInstance) GetVersion(ctx context.Context, key microconfig.Key, version string) (*microconfig.Item, error) {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return nil, err
	}
	// 版本号为空时返回统一错误。
	if strings.TrimSpace(version) == "" {
		return nil, microconfig.ErrInvalidItem
	}

	// 使用超时上下文执行读取。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 读取 versions ConfigMap。
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(reqCtx, s.versionsName(key), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, microconfig.ErrResourceNotFound
		}
		return nil, err
	}
	if cm.Data == nil {
		return nil, microconfig.ErrResourceNotFound
	}

	// 读取指定版本内容。
	raw, ok := cm.Data[version]
	if !ok || raw == "" {
		return nil, microconfig.ErrResourceNotFound
	}

	// 解析并返回配置内容。
	return s.decodeItem([]byte(raw))
}

// ListVersions 列出版本号列表。
func (s *StoreInstance) ListVersions(ctx context.Context, key microconfig.Key, limit int) ([]string, error) {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return nil, err
	}

	// 使用超时上下文执行读取。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 读取 versions ConfigMap。
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(reqCtx, s.versionsName(key), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return []string{}, nil
		}
		return nil, err
	}
	if cm.Data == nil {
		return []string{}, nil
	}

	// 提取版本号列表。
	versions := make([]string, 0, len(cm.Data))
	for version := range cm.Data {
		versions = append(versions, version)
	}

	// 按字典序倒排，确保新版本优先。
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))

	// 按 limit 截断结果。
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	return versions, nil
}

// GetMeta 读取配置元信息。
func (s *StoreInstance) GetMeta(ctx context.Context, key microconfig.Key) (*microconfig.Meta, error) {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return nil, err
	}

	// 使用超时上下文执行读取。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 读取 meta ConfigMap。
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(reqCtx, s.metaName(key), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, microconfig.ErrResourceNotFound
		}
		return nil, err
	}

	// 从固定数据键提取内容。
	raw, ok := cm.Data[dataKeyMeta]
	if !ok || raw == "" {
		return nil, microconfig.ErrResourceNotFound
	}

	// 解析并返回元信息。
	return s.decodeMeta([]byte(raw))
}

// PutMeta 写入配置元信息。
func (s *StoreInstance) PutMeta(ctx context.Context, key microconfig.Key, meta *microconfig.Meta) error {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return err
	}
	// meta 为空时返回统一错误。
	if meta == nil {
		return microconfig.ErrInvalidItem
	}

	// 编码元信息。
	val, err := s.encodeMeta(meta)
	if err != nil {
		return err
	}

	// 使用超时上下文执行写入。
	reqCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	// 确保 meta ConfigMap 存在并更新数据。
	return s.upsertConfigMapData(reqCtx, s.metaName(key), map[string]string{
		dataKeyMeta: string(val),
	}, map[string]string{
		"type": "meta",
	})
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
func (s *StoreInstance) currentName(key microconfig.Key) string {
	return fmt.Sprintf("cfg-current-%s", shortHash(buildKeySignature(key)))
}

// versionsName 生成 versions ConfigMap 名称。
func (s *StoreInstance) versionsName(key microconfig.Key) string {
	return fmt.Sprintf("cfg-versions-%s", shortHash(buildKeySignature(key)))
}

// metaName 生成 meta ConfigMap 名称。
func (s *StoreInstance) metaName(key microconfig.Key) string {
	return fmt.Sprintf("cfg-meta-%s", shortHash(buildKeySignature(key)))
}

// buildKeySignature 构造稳定的键签名字符串。
func buildKeySignature(key microconfig.Key) string {
	tenant := strings.TrimSpace(key.Tenant)
	if tenant == "" {
		tenant = "default"
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", tenant, key.Env, key.AppID, key.Group, key.Name)
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

// encodeItem 对配置内容做编码。
func (s *StoreInstance) encodeItem(item *microconfig.Item) ([]byte, error) {
	// 优先使用调用方注入的编解码器。
	if s.options != nil && s.options.Codec != nil {
		return s.options.Codec.Marshal(item)
	}
	// 默认使用 JSON 编码。
	return json.Marshal(item)
}

// decodeItem 对配置内容做解码。
func (s *StoreInstance) decodeItem(data []byte) (*microconfig.Item, error) {
	// 准备承载结果对象。
	raw := new(microconfig.Item)
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

// encodeMeta 对元信息做编码。
func (s *StoreInstance) encodeMeta(meta *microconfig.Meta) ([]byte, error) {
	// 优先使用调用方注入的编解码器。
	if s.options != nil && s.options.Codec != nil {
		return s.options.Codec.Marshal(meta)
	}
	// 默认使用 JSON 编码。
	return json.Marshal(meta)
}

// decodeMeta 对元信息做解码。
func (s *StoreInstance) decodeMeta(data []byte) (*microconfig.Meta, error) {
	// 准备承载结果对象。
	raw := new(microconfig.Meta)
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
func validateKey(key microconfig.Key) error {
	// Env 为空时视为无效 key。
	if strings.TrimSpace(key.Env) == "" {
		return microconfig.ErrInvalidKey
	}
	// AppID 为空时视为无效 key。
	if strings.TrimSpace(key.AppID) == "" {
		return microconfig.ErrInvalidKey
	}
	// Group 为空时视为无效 key。
	if strings.TrimSpace(key.Group) == "" {
		return microconfig.ErrInvalidKey
	}
	// Name 为空时视为无效 key。
	if strings.TrimSpace(key.Name) == "" {
		return microconfig.ErrInvalidKey
	}
	// key 校验通过。
	return nil
}
