package geecache

// 实现HTTP客户端，与远程节点的服务通信
// 实现之前的流程（2）：当缓存没有数据，选择是否应当从远程节点获取，进而与远程节点交互，返回缓存值

/*使用一致性哈希选择节点    是                                   是
  |-----> 是否是远程节点 -----> HTTP 客户端访问远程节点 --> 成功？-----> 服务端返回返回值
            |  否                                      ↓ 否
            |----------------------------> 回退到本地节点处理。*/

// PeerPicker 是必须实现的接口，由HTTPPool实现
type PeerPicker interface {
	// PickPeer 方法用于根据传入的key选择相应节点PeerGetter（http客户端）
	PickPeer(key string) (peer PeerGetter, ok bool)
}

// PeerGetter 是客户端接口，每个客户端必须实现Get方法
type PeerGetter interface {
	// Get 方法用于从对应的group查找缓存值
	Get(group string, key string) ([]byte, error)
}
