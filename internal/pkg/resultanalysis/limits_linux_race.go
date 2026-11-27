//go:build linux && race

package resultanalysis

import (
	"fmt"
	"runtime/debug"

	"golang.org/x/sys/unix"
)

// restrictMemory 在 Linux -race 构建下跳过 384 MiB 的 RLIMIT_DATA 限制：
// Linux 将 ThreadSanitizer 的匿名 shadow mmap 计入 RLIMIT_DATA，
// Go 1.26 TSan 运行时在测试子进程中会因此触发 ENOMEM（exit 66）。
// 非 race 生产构建仍由 limits_linux.go 强制执行完整 384 MiB RLIMIT_DATA 限制。
func restrictMemory() error {
	debug.SetMemoryLimit(256 << 20)
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		return fmt.Errorf("set analysis resource limit: %w", err)
	}
	return nil
}
