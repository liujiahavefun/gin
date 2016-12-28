package gin

import (
	"net/http"
	"os"
)

//liujia: 这个文件注意是用于静态文件服务的

//liujia: onlyfilesFS 可以视作实现了http.FileSystem接口，而neuteredReaddirFile实现了http.File接口
//如下Dir()函数，如果listDirectory为true，则返回正常的http.FileSystem
//如果为false，则返回onlyfilesFS包装后的http.FileSystem
//总体上就是返回一个只能读到空目录的东东
type (
	onlyfilesFS struct {
		fs http.FileSystem
	}
	neuteredReaddirFile struct {
		http.File
	}
)

// Dir returns a http.Filesystem that can be used by http.FileServer(). It is used interally
// in router.Static().
// if listDirectory == true, then it works the same as http.Dir() otherwise it returns
// a filesystem that prevents http.FileServer() to list the directory files.
func Dir(root string, listDirectory bool) http.FileSystem {
	fs := http.Dir(root)
	if listDirectory {
		return fs
	}
	return &onlyfilesFS{fs}
}

//liujia:相当于override了http.FileSystem的Open(name string)函数
//但注意，这里onlyfilesFS内部包含了一个非匿名的http.FileSystem成员，因为这里要调用真正的http.FileSystem的Open
//如果是匿名成员的话，就会出现递归调用，所以这里用非匿名的成员
//但下面neuteredReaddirFile因为就是要覆盖掉底层实现，所以直接用匿名成员就好了
//注：struct包含匿名成员，以为这默认包含了这个成员的所有成员，如果这个成员也是个struct的话，详情见书吧
// Conforms to http.Filesystem
func (fs onlyfilesFS) Open(name string) (http.File, error) {
	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return neuteredReaddirFile{f}, nil
}

// Overrides the http.File default implementation
func (f neuteredReaddirFile) Readdir(count int) ([]os.FileInfo, error) {
	// this disables directory listing
	return nil, nil
}
