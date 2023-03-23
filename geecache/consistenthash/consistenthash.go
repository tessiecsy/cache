package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
)

// 哈希函数，采用依赖注入的方式，允许用于替换成自定义的Hash函数，默认crc

type Hash func(data []byte) uint32

type Map struct {
	hash     Hash
	replicas int            // 虚拟节点倍数
	keys     []int          // 哈希环
	hashMap  map[int]string // 虚拟节点--真实节点 映射表  （键：虚拟节点的哈希值，值：真实节点的名称）
}

func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// 允许传入0/多个真实节点名称
// 对于每个真实节点key，创建replicas倍数个虚拟节点，strconv.Itoa(i)：添加编号，区分不同的虚拟节点
// m.hash 计算虚拟节点的哈希值，并添加到环上
// hashMap中添加虚拟--真实 的键值对
// 环上哈希值排序

func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		for i := 0; i < m.replicas; i++ {
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
			m.keys = append(m.keys, hash)
			m.hashMap[hash] = key
		}
	}
	sort.Ints(m.keys)
}

// 选择节点

func (m *Map) Get(key string) string {
	if len(m.keys) == 0 {
		return ""
	}

	hash := int(m.hash([]byte(key))) // 计算key的哈希值

	// 顺时针找到第一个匹配的虚拟节点的下标idx
	// search会找到【0，n）第一个符合条件的index， 如果没有，返回n
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})

	// 映射到真实节点，因为是环状结构，所以应该取余

	return m.hashMap[m.keys[idx%len(m.keys)]]
}
