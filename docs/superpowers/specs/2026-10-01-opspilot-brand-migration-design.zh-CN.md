# OpsPilot 品牌迁移设计

## 目标

基于最新的 upstream source 源码，创建一个独立的 OpsPilot 发行版。这是一次彻底的更名：运行时、构建、部署、打包、文档和发布标识符统一使用 `opspilot`；运行时不再接受旧的 legacy 标识符。

## 范围

- 将 Go 模块路径改为 `github.com/Zara1024/OpsPilot`，并更新内部导入。
- 将命令目录和二进制产物重命名为 `opspilot`、`opspilot-edge` 和 `opspilot-edge-supervisor`。
- 将环境变量从 legacy environment variables 重命名为 `OPSPILOT_*`，并更新所有读取处、示例、Compose 文件、安装脚本和测试。
- 将安装路径、数据、日志、Compose 项目名、容器名、卷名、证书和缓存的默认值从 legacy 重命名为 `opspilot`。
- 将 manager、web 和 Kubernetes Edge 镜像发布到 `docker.cnb.cool/zara1024/opspilot`，并将 Edge 发行版附件发布到 `zara1024/opspilot-edge`。
- 重命名安装包归档、发布元数据、下载 URL、Helm 引用、仪表盘标识符和面向用户的文档。
- 移除旧的兼容分支和旧品牌默认值，而不是新增别名。

## 实施规则

当标识符涉及 Go 导入、shell 变量、YAML 键、Docker 服务名或文件路径时，使用结构化替换。在更新引用之前先重命名文件系统路径。保持 `singchia/frontier` 等第三方名称不变。不要将凭据或令牌写入被版本控制的文件。

## 验证

1. 扫描受版本控制的文件，查找不区分大小写的 legacy；如有必要，仅允许保留一条明确的迁移说明。
2. 运行 `gofmt` 和 `go test ./...`。
3. 使用现有项目脚本运行前端依赖检查和生产构建。
4. 校验 Compose 和 shell 语法、Make 目标以及 GitHub workflow 引用。
5. 构建 manager 和 Edge 二进制文件，并核对其名称和内嵌的模块导入。
6. 审查 OpsPilot CNB 仓库的发布配置，并确认工作区中不含任何密钥。

## 部署假设

本发行版面向全新的 OpsPilot 安装。已有的 legacy 安装、卷、环境文件和容器不在兼容性承诺范围内，需要手动迁移。
