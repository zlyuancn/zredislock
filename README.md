# zredislock
> redis的并发锁

## 获得zredislock
> `go get -u github.com/zlyuancn/zredislock`

## 使用zredislock

```go
// 创建redis客户端
client := redis.NewClient(&redis.Options{
    Addr:     "127.0.0.1:6379",
})
defer client.Close()

// 创建锁生成器
zl := zredislock.New(client)

// 获取一个锁
lock, err := zl.Obtain("lock", 300, 0)
if err != nil {
    panic(err)
}
defer lock.Release() // 别忘记释放锁

// 自动刷新
lock.AutoRefresh(300)

// 模拟程序处理
time.Sleep(5e9)
```
