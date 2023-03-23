package geecache

// 只读数据结构ByteView，表示缓存值

type ByteView struct {
	b []byte // b存储真实的缓存值
}

func (v ByteView) Len() int {
	return len(v.b)
}

// b是只读的，使用ByteSlice() 方法返回一个拷贝，防止缓存值被外部程序修改

func (v ByteView) ByteSlice() []byte {
	return cloneBytes(v.b)
}

func (v ByteView) String() string {
	return string(v.b)
}

func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
