package geecache

import (
	"fmt"
	"geecache/geecache/singleflight"
	"log"
	"sync"
)

// 负责与外部交互，控制缓存存储和获取的主流程

/*                        是
接收 key --> 检查是否被缓存 -----> 返回缓存值 ⑴
               |  否                        是
               |-----> 是否应当从远程节点获取 -----> 与远程节点交互 --> 返回缓存值 ⑵
                           |  否
                           |-----> 调用`回调函数`，获取值并添加到缓存 --> 返回缓存值 ⑶*/

/*一个 Group 可以认为是一个缓存的命名空间，每个 Group 拥有一个唯一的名称 name
比如可以创建三个 Group，缓存学生的成绩命名为 scores，缓存学生信息的命名为 info，缓存学生课程的命名为 courses*/

type Group struct {
	name      string
	getter    Getter     // 缓存未命中时获取源数据的回调（callback）
	mainCache cache      // 之前实现的并发缓存
	peers     PeerPicker // HTTPPool对象，实现了PeerPicker，记录可访问的远程节点

	loader *singleflight.Group
}

// 回调Getter

type Getter interface {
	Get(key string) ([]byte, error)
}

// 定义函数类型 GetterFunc，并实现 Getter 接口的 Get 方法
// 函数类型实现某一个接口，称之为接口型函数，方便使用者在调用时既能够传入函数作为参数，也能够传入实现了该接口的结构体作为参数

type GetterFunc func(key string) ([]byte, error)

func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group) // 多个不同名称的Group缓存空间组成groups
)

// NewGroup 函数实例化Group，并且将group存储在全局变量groups中
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock()
	defer mu.Unlock()
	g := &Group{
		name:      name,
		getter:    getter,
		mainCache: cache{cacheBytes: cacheBytes},
		loader:    &singleflight.Group{},
	}
	groups[name] = g
	return g
}

// GetGroup 用来获取特定名称的Group，只读锁RLock，不涉及冲突变量的写操作
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("key is required")
	}

	// 从mainCache里面拿缓存， 如果能拿到就返回缓存值
	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache hit]")
		return v, nil
	}
	return g.load(key) // 缓存里没有，load去其他节点拿 or 回调函数去数据库拿
}

// RegisterPeers 实现了 PeerPicker 接口的 HTTPPool 注入到 Group 中
func (g *Group) RegisterPeers(peers PeerPicker) {
	if g.peers != nil {
		panic("RegisterPeerPicker called more than once")
	}
	g.peers = peers
}

// 使用PickPeer方法选择节点，若非本机节点，则调用getFromPeer从远程获取，若是本机节点或失败，则回退到getLocally
// 若缓存值在远程节点上存在，则用对应的HTTP客户端从远程节点上访问获取缓存值
// 若不能则调用回调函数，获取值并添加到缓存
func (g *Group) load(key string) (value ByteView, err error) {
	// 这时在并发场景下针对相同的key，load过程只会调用一次
	viewi, err := g.loader.Do(key, func() (interface{}, error) {
		if g.peers != nil {
			// 通过一致性哈希找到存储key的节点客户端peer
			if peer, ok := g.peers.PickPeer(key); ok {
				// 利用HTTP客户端访问远程节点
				if value, err = g.getFromPeer(peer, key); err == nil {
					return value, nil
				}
				log.Println("[GeeCache] Failed to get from peer", err)
			}
		}
		return g.getLocally(key) // 调用用户回调函数，获取源数据
	})

	if err == nil {
		return viewi.(ByteView), nil
	}
	return
}

// getFromPeer 使用实现了PeerGetter接口的httpGetter访问远程节点，获取缓存值
func (g *Group) getFromPeer(peer PeerGetter, key string) (ByteView, error) {
	bytes, err := peer.Get(g.name, key)
	if err != nil {
		return ByteView{}, err
	}
	return ByteView{b: bytes}, nil
}

func (g *Group) getLocally(key string) (ByteView, error) {
	bytes, err := g.getter.Get(key) // 回调函数返回的bytes是[]byte类型，切片指向同一地址，需要clone，防止被外部程序改变
	if err != nil {
		return ByteView{}, err
	}
	value := ByteView{b: cloneBytes(bytes)}
	g.populateCache(key, value) // 添加到缓存mainCache中
	return value, nil
}

func (g *Group) populateCache(key string, value ByteView) {
	g.mainCache.add(key, value)
}
