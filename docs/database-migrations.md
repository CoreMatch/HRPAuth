# 数据库迁移与 Schema Sync

本文档说明本项目当前的数据库 schema 管理方式、迁移命令，以及“空库初始化”和“已有共享库纳管”的标准流程。

## 目标

- **Schema 真源**：SQL migration
- **迁移工具**：`golang-migrate`
- **执行入口**：`go run ./cmd/migrate ...`
- **运行时策略**：应用启动时**不再**执行 `AutoMigrate`

## 当前目录

| 路径 | 作用 |
|------|------|
| `cmd/migrate/main.go` | 迁移命令入口 |
| `database/migrations/000001_baseline.up.sql` | 共享库现状的 baseline 快照 |
| `database/migrations/000001_baseline.down.sql` | baseline 回滚 |
| `database/migrations/000002_add_cbh_and_mojang_uuid.*.sql` | `users.cbh` / `users.mojang_uuid` |
| `database/migrations/000003_add_mbe.*.sql` | `users.mbe` |

## 命令用法

所有命令都从仓库根目录执行，并复用 `config.yaml` 中的 `database.*` 配置。

```bash
go run ./cmd/migrate version
go run ./cmd/migrate status
go run ./cmd/migrate up
go run ./cmd/migrate up 1
go run ./cmd/migrate down
go run ./cmd/migrate down 1
go run ./cmd/migrate force 1
```

### 命令说明

| 命令 | 含义 |
|------|------|
| `version` / `status` | 查看当前 migration 版本与 dirty 状态 |
| `up` | 执行全部未应用 migration |
| `up N` | 仅向前执行 `N` 步 |
| `down` | 默认回滚 1 步 |
| `down N` | 回滚 `N` 步 |
| `force VERSION` | 强制把数据库版本标记到指定版本，不执行 SQL |

## Baseline 规则

`000001_baseline` 的设计目标是**忠实冻结共享库现状**，不是理想化重建。

- 保留共享库当前的 5 张表：`users`、`profiles`、`profile_properties`、`sessions`、`tokens`
- 保留共享库已有外键
- 保留共享库已有重复索引，后续单独 migration 再清理
- 保留 `users` 上的 Blessing Skin 兼容遗留字段，后续单独 migration 再清理
- 不包含 dump 中的数据语句
- 不包含 `DROP TABLE`、`LOCK TABLES`、`INSERT` 等不适合作为 `up` migration 的语句
- 不保留 dump 中当时的 `AUTO_INCREMENT=...` 计数

> 注意：`user_properties` 当前既不在共享库中存在，也没有任何运行时代码读写，因此不纳入 baseline。

## 空库初始化

适用于全新数据库，且目标结构应从 baseline 开始构建。

1. 准备好 `config.yaml`，确保 `database.*` 指向目标库。
2. 在仓库根目录执行：

```bash
go run ./cmd/migrate up
```

3. 验证版本：

```bash
go run ./cmd/migrate status
```

预期结果：
- 版本为当前最新 migration
- `dirty: false`

## 已有共享库纳管

适用于“数据库已经存在，且结构应被视为 baseline 现状”的场景。

### 标准流程

1. **先备份数据库**。
2. 确认现有库结构与 `000001_baseline` 对应的共享库快照一致。
3. **不要**直接在现有共享库上执行 `go run ./cmd/migrate up`，否则会尝试重建 baseline 表。
4. 先执行：

```bash
go run ./cmd/migrate force 1
```

5. 再执行：

```bash
go run ./cmd/migrate up
```

6. 最后检查状态：

```bash
go run ./cmd/migrate status
```

### 为什么先 `force 1`

因为 `000001_baseline` 是“共享库当时结构”的快照。  
对已经存在这些表的库来说，正确做法不是重复执行 baseline，而是把 migration 系统的版本**对齐到 baseline 已完成**，然后只执行后续真正的增量变更。

## 当前增量迁移

### `000002_add_cbh_and_mojang_uuid`

- 给 `users` 增加 `cbh`
- 给 `users` 增加 `mojang_uuid`
- 给 `mojang_uuid` 增加唯一索引 `uk_users_mojang_uuid`
- `mojang_uuid` 使用 `CHARACTER SET ascii COLLATE ascii_bin`

### `000003_add_mbe`

- 给 `users` 增加 `mbe`

## 运行时约束

- 应用启动不负责改表；数据库初始化仅建立连接
- 所有 schema 变更必须通过 `database/migrations/` 落地
- 修改 GORM 模型时，必须同时评估是否需要新增 migration
- 对已有共享库做 schema sync 前，必须先确认目标库版本和 baseline 对齐方式

## 后续整理项

- 继续处理代码模型与共享库的 drift，例如 `profiles.name` 长度、重复索引、字段可空性
- 规划 `users` 上 Blessing Skin 兼容遗留字段的下线 migration
- 如需继续精简 schema，可在确认无业务依赖后移除更多历史兼容结构
