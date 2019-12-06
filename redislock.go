/*
-------------------------------------------------
   Author :       Zhang Fan
   date：         2019/10/19
   Description :
-------------------------------------------------
*/

package zredislock

import (
    "context"
    "fmt"
    "github.com/zlyuancn/zerrors"
    "strconv"
    "sync"
    "time"

    "github.com/go-redis/redis"
    "github.com/satori/go.uuid"
)

var (
    luaRefresh = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`)
    luaRelease = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)
)

type Client struct {
    client redis.UniversalClient
    mx     sync.Mutex
}

const (
    DefaultObtainLoopWait = 0.1e9
)

func New(client redis.UniversalClient) *Client {
    return &Client{client: client}
}

func (m *Client) Obtain(key string, ttl int64, timeout time.Duration) (*Lock, error) {
    if ttl <= 0 {
        return nil, zerrors.New("TTL必须大于0")
    }

    value := uuid.NewV4().String()

    lock, err := m.obtain(key, value, ttl)
    if err != nil {
        return nil, zerrors.Wrap(err, "无法获取锁")
    } else if lock != nil {
        return lock, nil
    }

    timer := time.NewTicker(time.Duration(DefaultObtainLoopWait))
    defer timer.Stop()

    ctx := context.Background()
    cancel := func() {}
    if timeout > 0 {
        ctx, cancel = context.WithTimeout(ctx, timeout)
    }
    defer cancel()

    for {
        select {
        case <-timer.C:
            lock, err := m.obtain(key, value, ttl)
            if err != nil {
                return nil, zerrors.Wrap(err, "无法获取锁")
            } else if lock != nil {
                return lock, nil
            }
        case <-ctx.Done():
            return nil, zerrors.New("获取锁超时")
        }
    }
}

func (m *Client) obtain(key, value string, ttl int64) (*Lock, error) {
    ok, err := m.client.SetNX(key, value, time.Duration(ttl*1e6)).Result()
    if err != nil && err != redis.Nil {
        return nil, err
    }

    if ok {
        return newLock(m, key, value), nil
    }
    return nil, nil
}

func Obtain(client *redis.Client, key string, ttl int64, timeout time.Duration) (*Lock, error) {
    return New(client).Obtain(key, ttl, timeout)
}

type Lock struct {
    client  *Client
    key     string
    value   string
    release chan struct{}
}

func newLock(client *Client, key string, value string) *Lock {
    return &Lock{
        client:  client,
        key:     key,
        value:   value,
        release: make(chan struct{}, 1),
    }
}

func (m *Lock) Key() string {
    return m.key
}

func (m *Lock) Token() string {
    return m.value
}

func (m *Lock) refresh(ttl int64) error {
    ttlVal := strconv.FormatInt(ttl, 10)
    status, err := luaRefresh.Run(m.client.client, []string{m.key}, m.value, ttlVal).Result()
    if err != nil {
        return zerrors.Wrap(err, "刷新TTL脚本执行失败")
    } else if status == int64(1) {
        return nil
    }
    return zerrors.New("刷新TTL失败, 锁不存在或键的值已被改变")
}

// 自动刷新TTL(毫秒)
func (m *Lock) AutoRefresh(ttl int64) (cancel func()) {
    if ttl <= 0 {
        return nil
    }

    stop := make(chan struct{}, 1)
    go func() {
        timer := time.NewTicker(time.Duration(ttl * 1e6 / 3))
        defer timer.Stop()
        for {
            select {
            case <-timer.C:
                if err := m.refresh(ttl); err != nil {
                    fmt.Println(err)
                    return
                }
            case <-m.release:
                return
            case <-stop:
                return
            }
        }
    }()
    return func() {
        stop <- struct{}{}
    }
}

// 释放锁
func (m *Lock) Release() error {
    _, err := luaRelease.Run(m.client.client, []string{m.key}, m.value).Result()
    m.release <- struct{}{}

    if err == redis.Nil {
        return nil
    } else if err != nil {
        return zerrors.Wrap(err, "释放锁脚本执行失败")
    }

    return nil
}
