package classic

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// mustReadSource 读取仓库内某个源码文件，用于结构性断言。
func mustReadSource(t *testing.T, rel string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("读取 %s: %v", rel, err)
	}
	return string(b)
}
