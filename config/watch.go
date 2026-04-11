package config

import (
	"context"
	"time"

	microConfig "github.com/fireflycore/go-micro/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
)

// Watch 监听指定配置键的变更事件。
func (s *StoreInstance) Watch(ctx context.Context, key microConfig.Key) (<-chan microConfig.WatchEvent, error) {
	// key 不合法时直接返回统一错误。
	if err := validateKey(key); err != nil {
		return nil, err
	}
	// 空上下文回退为 Background。
	if ctx == nil {
		ctx = context.Background()
	}

	// 计算 current 资源名，并作为 watch 唯一标识。
	resourceName := s.currentName(key)

	// 为该 watch 创建可取消上下文。
	watchCtx, cancel := context.WithCancel(ctx)

	// 把取消函数记录到 map，供 Unwatch 调用。
	s.watchMu.Lock()
	s.watchCancels[resourceName] = cancel
	s.watchMu.Unlock()

	// 计算输出通道缓冲区大小。
	bufferSize := 8
	if s.options != nil && s.options.WatchBuffer > 0 {
		bufferSize = s.options.WatchBuffer
	}
	out := make(chan microConfig.WatchEvent, bufferSize)

	// 启动异步 watch 循环。
	go func() {
		// 关闭前移除取消函数，避免泄漏。
		defer func() {
			s.watchMu.Lock()
			delete(s.watchCancels, resourceName)
			s.watchMu.Unlock()
			close(out)
		}()

		for {
			// 上下文结束时退出循环。
			select {
			case <-watchCtx.Done():
				return
			default:
			}

			// 只监听单个 ConfigMap 名称，减少事件噪音。
			selector := fields.OneTermEqualSelector("metadata.name", resourceName).String()
			watcher, err := s.client.CoreV1().ConfigMaps(s.namespace).Watch(watchCtx, metav1.ListOptions{
				FieldSelector: selector,
			})
			if err != nil {
				// 创建 watch 失败时退避，避免空转。
				timer := time.NewTimer(300 * time.Millisecond)
				select {
				case <-watchCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
					continue
				}
			}

			// 消费当前 watch stream。
			closed := s.consumeWatchStream(watchCtx, key, watcher, out)
			if closed {
				return
			}
		}
	}()

	// 返回监听通道。
	return out, nil
}

// Unwatch 取消指定配置键的监听。
func (s *StoreInstance) Unwatch(key microConfig.Key) {
	// key 不合法时直接忽略，保持幂等。
	if err := validateKey(key); err != nil {
		return
	}

	// 查找对应 cancel 并触发取消。
	resourceName := s.currentName(key)
	s.watchMu.Lock()
	cancel, ok := s.watchCancels[resourceName]
	s.watchMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

// consumeWatchStream 消费单次 watch stream，返回 true 表示应退出主循环。
func (s *StoreInstance) consumeWatchStream(
	ctx context.Context,
	key microConfig.Key,
	watcher watch.Interface,
	out chan<- microConfig.WatchEvent,
) bool {
	// 确保当前 stream 结束时关闭 watcher。
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return true
		case event, ok := <-watcher.ResultChan():
			// stream 结束，交由上层重建。
			if !ok {
				return false
			}

			// 把 K8s 事件转换为统一事件。
			watchEvent, valid := s.toWatchEvent(key, event)
			if !valid {
				continue
			}

			// 发送时支持上下文取消，避免阻塞。
			select {
			case <-ctx.Done():
				return true
			case out <- watchEvent:
			}
		}
	}
}

// toWatchEvent 把 K8s Watch 事件转换为统一 watch 事件。
func (s *StoreInstance) toWatchEvent(key microConfig.Key, event watch.Event) (microConfig.WatchEvent, bool) {
	// 删除事件直接返回 EventDelete。
	if event.Type == watch.Deleted {
		return microConfig.WatchEvent{
			Type: microConfig.EventDelete,
			Key:  key,
		}, true
	}

	// 其余事件尝试解析 ConfigMap 内容。
	cm, ok := event.Object.(*corev1.ConfigMap)
	if !ok || cm == nil || cm.Data == nil {
		return microConfig.WatchEvent{}, false
	}
	raw, ok := cm.Data[dataKeyRaw]
	if !ok || raw == "" {
		return microConfig.WatchEvent{}, false
	}
	rawValue, err := s.decodeRaw([]byte(raw))
	if err != nil {
		return microConfig.WatchEvent{}, false
	}

	// 返回统一 put 事件。
	return microConfig.WatchEvent{
		Type: microConfig.EventPut,
		Key:  key,
		Raw:  rawValue,
	}, true
}
