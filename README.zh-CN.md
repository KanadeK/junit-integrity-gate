# junit-integrity-gate

在 CI 接受 JUnit 报告之前，检查预期分片缺失、重复报告和统计矛盾。
本地命令行工具；Go 标准库实现；运行无需 Go、Python、账号或外部服务。
完整参数、格式和边界见 [英文 README](README.md)。

## 使用

从 [v0.1.0 Release](https://github.com/KanadeK/junit-integrity-gate/releases/tag/v0.1.0)
下载对应 Windows x64、Linux x64 或 macOS Intel/Apple Silicon 的 ZIP，
对照 `SHA256SUMS.txt` 校验后解压。Windows 用 `Get-FileHash`，Linux 用
`sha256sum`，macOS 用 `shasum -a 256`。保留随包许可证。

也可安装 Go 1.27.3 或更新的受维护安全补丁，从源码构建：

```sh
git clone --branch v0.1.0 https://github.com/KanadeK/junit-integrity-gate.git
cd junit-integrity-gate
go build -trimpath -buildvcs=false -o build/ ./cmd/junit-integrity-gate
```

在源码或解压目录运行完整示例；按环境将命令换成 `./junit-integrity-gate`
或 `.\junit-integrity-gate.exe`，也可把二进制放进 PATH：

```sh
junit-integrity-gate check --json examples/expected.json
```

示例应检查两个文件，得到 3 条记录、2 通过、1 跳过，退出码 0。
自己的预期清单格式如下，文件路径相对清单所在目录：

```json
{"version":1,"files":["reports/shard-1.xml","reports/shard-2.xml"]}
```

清单应来自测试任务的预期分片。不要用“当前找到的 XML”生成预期清单，
否则已丢失的分片不会进入检查范围。参数放在清单路径之前：

```sh
junit-integrity-gate check --json expected.json
```

默认跨文件比较精确 `(classname, name)`，发现复制分片或重复用例即失败。
平台矩阵确需不同文件重复同一用例时，显式使用 `--duplicate-scope file`；
文件内重复和同一个物理文件重复输入仍然失败。

## 检查与限制

每个预期文件必须存在，并至少有一条非跳过记录。每个 `testsuite` 的
`tests` 必填，等于其所有后代用例数；可选失败、错误和跳过声明一旦提供，
都要与观察值一致。失败、错误、矛盾结果、空报告、全跳过、格式不支持或
资源超限都会阻止通过。日志、属性和错误文本不会进入输出。

支持结构无命名空间、无重试的经典 JUnit 子集。拒绝 DTD、非 UTF-8、重试扩展、
未知结果节点及报告目录内的符号链接路径。普通 pytest xunit2 和 unittest-xml-reporting
由实际生成报告的验收流程验证；不同生产者、合并重试、pytest 双阶段错误
可能不符合本项目严格策略。不得把它当作通用 JUnit 标准验证器。

退出码：0 符合策略；1 报告/文件/策略失败；2 命令、清单或配置错误。
JSON 带 `version:1`，输出计数、固定错误码和文件/套件/用例序号。
失败时计数可能不完整。默认上限见英文 README，包含不解析语义的日志子树。

工具不会运行测试，无法证明报告新鲜度、真实性、测试覆盖清单或提交来源。
需要保留测试进程退出码，并在 CI 中绑定正确运行批次。输入应是自己控制
目录内已写完的稳定普通文件；文件检查不是并发文件系统的原子快照，
也不是内存或严格超时沙箱。只读输入，无网络和遥测调用。

卸载时删除解压目录及放入 PATH/Go bin 的程序即可。只清理自己创建的
示例副本。工具不会创建配置、缓存或后台服务。

原始代码采用 MIT；二进制同时包含 Go 运行时的 BSD 许可声明。
实际测试范围见 [VALIDATION.md](VALIDATION.md)，贡献及安全报告方式见
[CONTRIBUTING.md](CONTRIBUTING.md) 和 [SECURITY.md](SECURITY.md)。
