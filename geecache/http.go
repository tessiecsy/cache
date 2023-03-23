package geecache

import (
	"fmt"
	"geecache/geecache/consistenthash"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// 基于http， 提供被其他节点访问的能力，如果一个节点启动了它的HTTP服务端，那么它就可以被其他节点访问

const (
	defaultBasePath = "/_geecache/"
	defaultReplicas = 50
)

// 约定访问路径格式为/<basepath>/<groupname>/<key>
// 通过groupname得到group实例， 再使用group.Get(key)获取缓存数据

type HTTPPool struct {
	self     string // 自己的地址，包括ip + port
	basePath string //节点间通信地址的前缀
	mu       sync.Mutex
	peers    *consistenthash.Map // 根据具体的key选择节点

	// 键是"http://10.0.0.2:8008"，值是对应的HTTP客户端
	// 即，从一致性哈希里面找到了key存在"http://10.0.0.2:8008"这个远程节点上，利用此字段就可获取到访问这个远程节点的HTTP客户端
	httpGetters map[string]*httpGetter
	// 映射远程节点与对应的 httpGetter。每一个远程节点对应一个 httpGetter，因为 httpGetter 与远程节点的地址 baseURL 有关
}

func NewHTTPPool(self string) *HTTPPool {
	return &HTTPPool{
		self:     self,
		basePath: defaultBasePath,
	}
}

func (p *HTTPPool) Log(format string, v ...interface{}) {
	log.Printf("Server %s] %s", p.self, fmt.Sprintf(format, v...))
}

func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 判断访问路径的前缀是否是basepath， 如果不是返回错误信息
	if !strings.HasPrefix(r.URL.Path, p.basePath) {
		panic("HTTPPool serving unexpected path: " + r.URL.Path)
	}
	p.Log("%s %s", r.Method, r.URL.Path)

	// <basepath>/<groupname>/<key>
	// 第一个参数实际的输入是<groupname>/<key>
	// 然后，/作为分隔符，将字符串分割出2个子串，即<groupname>和<key>
	parts := strings.SplitN(r.URL.Path[len(p.basePath):], "/", 2)
	// 如果不是<groupname>和<key>，则规则不匹配，返回错误
	if len(parts) != 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// 分别取出字符串
	groupName := parts[0]
	key := parts[1]

	// 返回指定name的group
	group := GetGroup(groupName)
	if group == nil {
		http.Error(w, "no such group"+groupName, http.StatusNotFound)
		return
	}

	// 根据key值取缓存
	view, err := group.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 设置httpResponse的头部
	// Content-Type：内容类型：application/octet-stream：二进制流数据（如常见的文件下载）
	w.Header().Set("Content-Type", "application/octet-stream")
	// 将缓存值作为httpResponse的body返回
	w.Write(view.ByteSlice())
}

// Set 实例化了一致性哈希算法, 并且添加了传入的节点，并为每一个节点创建了一个HTTP客户端httpGetter
// peers是一个字符串数组["http://localhost:8001","http://localhost:8002","http://localhost:8003"]
func (p *HTTPPool) Set(peers ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peers = consistenthash.New(defaultReplicas, nil)
	p.peers.Add(peers...)
	p.httpGetters = make(map[string]*httpGetter, len(peers))
	for _, peer := range peers {
		p.httpGetters[peer] = &httpGetter{baseURL: peer + p.basePath}
	}
}

// PickPeer 实现了PeerPicker接口，在哈希环上找key对应的节点，然后返回这个节点的http客户端
func (p *HTTPPool) PickPeer(key string) (PeerGetter, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Get方法是在一致性哈希上面找存储key的节点，返回的peer是string，如"http://localhost:8001"
	if peer := p.peers.Get(key); peer != "" && peer != p.self {
		p.Log("Pick peer %s", peer)
		return p.httpGetters[peer], true
	}
	return nil, false
}

// 检查 HTTPPool 是否实现了接口 PeerPicker ，若没有则会编译出错
var _PeerPicker = (*HTTPPool)(nil)

// 客户端类httpGetter
type httpGetter struct {
	baseURL string // 表示要访问的远程节点的地址
}

// Get 客户端httpGetter根据group和key返回缓存值
func (h *httpGetter) Get(group string, key string) ([]byte, error) {
	// 进行字符串拼接， 格式：http://example.com/_geecache/group/key
	u := fmt.Sprintf(
		"%v%v%v",
		h.baseURL,
		url.QueryEscape(group),
		url.QueryEscape(key),
	)
	// 发起http的get请求，访问远程节点
	res, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	// 检测状态码
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned: %v", res.Status)
	}
	// 读取消息体的响应内容
	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %v", err)
	}

	return bytes, nil

}

// 检查httpGetter是否实现了接口PeerGetter，若没有则会编译出错
var _PeerGetter = (*httpGetter)(nil)

// HTTPPool 既具备了提供 HTTP 服务的能力，也具备了根据具体的 key，创建 HTTP 客户端从远程节点获取缓存值的能力。
