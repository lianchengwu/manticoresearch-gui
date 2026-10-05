# ManticoreSearch GUI

基于 **Wails3 + Vue 3 + TypeScript** 与官方 SDK **[manticoresearch-go](https://github.com/manticoresoftware/manticoresearch-go)** 的 Manticore Search 桌面客户端。

![wails3](https://img.shields.io/badge/wails-v3.0.0--beta.24-blue) ![go](https://img.shields.io/badge/go-1.25%2B-00add8) ![vue](https://img.shields.io/badge/vue-3-42b883)

## 功能

- **SQL 控制台** — 执行任意 Manticore SQL(`Ctrl+Enter` / `F8` 运行),查询历史(本地持久化),结果网格保留服务端列顺序
- **表浏览** — 侧边栏列出所有表(类型徽标:rt / percolate / distributed…),数据浏览支持全文检索(query_string)、排序、分页
- **文档管理** — 通过 Index API 插入 / 替换 / 删除文档,JSON 编辑器,行详情抽屉
- **建表** — 可视化设计实时表 / 渗滤表 / 分布式表，预览并执行 `CREATE TABLE`，也可直接改 SQL
- **设计表** — 对已有表新增 / 删除字段、把 int 扩成 bigint、修改全文设置、重命名，变更以 `ALTER` 逐条应用
- **表操作** — 侧栏右键或「操作」菜单：优化磁盘块、刷出内存块、强制落盘、状态、设置、复制结构、清空、删除
- **表结构** — `DESCRIBE` 字段网格 + `SHOW CREATE TABLE` 建表语句
- **集群模式** — 连接可配置多个 Manticore 节点,请求轮询分发,节点故障自动转移
- **网络链路拓扑** — HTTP 代理、SOCKS5 代理、SSH 隧道可**混合编排为任意顺序的穿透链**(本机 → 跳 1 → 跳 2 → … → Manticore),支持启停、排序、SSH 私钥/密码认证、TOFU 主机密钥校验
- **无系统标题栏** — 自定义标题栏(拖拽区、最小化/最大化/关闭)
- **深色 / 浅色主题** — 一键切换,自动记忆

## 开发

```bash
# 开发模式(热重载)
wails3 task dev

# 构建桌面应用
wails3 task build          # 产物: bin/manticoresearch-gui

# 构建 server 模式(无 GUI,纯 HTTP,便于 CI / 浏览器调试)
wails3 task build:server   # 产物: bin/manticoresearch-gui-server (默认 :8080)

# 后端测试(含模拟 Manticore 与代理链的集成测试)
go test ./...

# 启动本地模拟 Manticore(开发 / 演示用)
go run ./cmd/mockmanticore          # 默认 127.0.0.1:9308
```

前端修改后需重新生成绑定:

```bash
wails3 generate bindings
```

## 架构

```
main.go            应用入口:无边框窗口、服务绑定
connection.go      连接管理(持久化到 ~/.config/manticoresearch-gui/)+ 连通性测试
netdial.go         网络层:HTTP/SOCKS5/SSH 混合穿透链 + 集群轮询故障转移
query.go           SQL 执行(UtilsAPI.Sql)
table.go           表与文档服务(SearchAPI / IndexAPI / SQL)
ddl.go             建表 / 改表 SQL 生成与校验
normalize.go       响应归一化:保序 JSON 解析,统一各种响应形状为 QueryResult
frontend/
  src/lib/api.ts   绑定类型收敛层
  src/stores/      轻量响应式状态仓库
  src/components/  TitleBar / Sidebar / SqlConsole / TableData / TableSchema /
                   TableDesigner / DataGrid / ConnectionModal / DocModal / Toasts / ConfirmModal
```

### 网络链路语义

- 链路按列表顺序自左向右穿透;每个节点的穿透目标默认"自动" = 下一跳(最后一跳指向 Manticore),也可手工指定
- SSH 节点复用已认证会话(`ssh.Client` 按 hop 配置缓存),主机密钥采用 TOFU(首次信任并记录指纹,变更时拒绝并可一键清除记录)
- 集群与穿透链正交:集群先按节点轮询,再穿过同一条链路
- 密码 / 私钥口令保存在本地配置文件(明文,与常见数据库客户端一致),请注意文件权限(0600)

## 说明

- SDK 生成代码无法表达 Manticore 的全部响应形状(如浮点 `_score`),`normalize.go` 在原始 JSON 层做保序解码兜底,列顺序与服务器一致
- percolate 表的字符串文档 id 请通过 SQL 控制台管理(文档 CRUD 面板仅支持数值 id)
