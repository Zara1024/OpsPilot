//go:build windows

package edgedirs

// Windows 部署目录约定。
//
//   - binary → C:\Program Files\opspilot-edge\bin\
//   - data   → C:\ProgramData\opspilot-edge\
//
// ProgramData 是 Windows Service 标准数据目录，NetworkService 可写。
const (
	BinDir        = `C:\Program Files\opspilot-edge\bin`
	DataDir       = `C:\ProgramData\opspilot-edge`
	PluginWorkDir = `C:\ProgramData\opspilot-edge\plugins`
	StageDir      = `C:\ProgramData\opspilot-edge\upgrade`
)

// HealthFile 是 worker.exe 与 supervisor.exe 之间的 health.json IPC 文件路径。
const HealthFile = DataDir + `\health.json`

// WorkerBinary 是 worker.exe 在 BinDir 下的文件名。
const WorkerBinary = "opspilot-edge-worker.exe"
