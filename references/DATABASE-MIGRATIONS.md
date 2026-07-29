# 数据库迁移（AI 简洁版）

> 完整开发文档（含设计决策、基线规则、增量说明）见 [`../docs/database-migrations.md`](../docs/database-migrations.md)

## 命令

从仓库根目录执行，复用 `config.yaml` 的 `database.*`。

```bash
go run ./cmd/migrate version          # 查看版本与 dirty 状态
go run ./cmd/migrate status           # 同上
go run ./cmd/migrate up               # 执行全部未应用 migration
go run ./cmd/migrate up 1             # 仅向前 1 步
go run ./cmd/migrate down             # 回滚 1 步
go run ./cmd/migrate down 1           # 回滚 N 步
go run ./cmd/migrate force 1          # 强制标记版本（不执行 SQL）
```

## 迁移清单

| 编号 | 内容 |
|------|------|
| 000001 | baseline：5 张表最终 Schema（已合并 000002～000007 所有增量变更） |

## 空库初始化

```bash
go run ./cmd/migrate up
go run ./cmd/migrate status   # 版本=1，dirty: false
```

## 已有共享库纳管

```bash
go run ./cmd/migrate force 1  # 对齐 baseline，不执行 SQL
go run ./cmd/migrate status
```

## 运行时约束

- 应用不执行 `AutoMigrate`
- 改 schema 必须走 `database/migrations/`
- 改模型必须评估是否需要新 migration
