// repo 包 — 数据访问层
//
// 提供 SQLite 各表的 CRUD 操作。
// 所有 repository 接收 *sql.DB 参数，不包含 HTTP 逻辑。
// 事务管理由调用方（service 层）负责。
package repo
