# 数据库迁移与 Schema Sync

本文档说明本项目当前的数据库 schema 管理方式、迁移命令，以及"空库初始化"和"已有共享库纳管"的标准流程。

## 目标

- **Schema 真源**：SQL migration
- **迁移工具**：`golang-migrate`
- **执行入口**：`go run ./cmd/migrate ...`
- **运行时策略**：应用启动时**不再**执行 `AutoMigrate`

## 当前目录

| 路径 | 作用 |
|------|------|
| `cmd/migrate/main.go` | 迁移命令入口 |
| `database/migrations/000001_baseline.up.sql` | 合并后的 baseline：5 张表最终 Schema |
| `database/migrations/000001_baseline.down.sql` | baseline 回滚 |

所有历史增量迁移（000002～000007）已合并至 `000001_baseline`，不再单独保留。

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

## Baseline 说明

`000001_baseline` 是当前数据库 schema 的最终状态快照，包含了此前所有增量迁移（000002～000007）的累积效果。

包含 5 张表：`users`、`profiles`、`profile_properties`、`sessions`、`tokens`

## 空库初始化

适用于全新数据库。

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
- 版本为 1
- `dirty: false`

## 已有共享库纳管

适用于"数据库已存在且结构已对齐"的场景（即唯一部署的数据库）。

### 标准流程

1. **先备份数据库**。
2. 确认现有库结构与 `000001_baseline` 一致。
3. 执行：

```bash
go run ./cmd/migrate force 1
```

4. 验证状态：

```bash
go run ./cmd/migrate status
```

预期结果：
- 版本为 1
- `dirty: false`

### 为什么先 `force 1`

因为 `000001_baseline` 是当前 schema 的快照，表中已存在数据。对已经存在这些表的库，正确做法不是重复执行 baseline，而是把 migration 系统的版本**对齐到 baseline 已完成**。

## 运行时约束

- 应用启动不负责改表；数据库初始化仅建立连接
- 所有 schema 变更必须通过 `database/migrations/` 落地
- 修改 GORM 模型时，必须同时评估是否需要新增 migration
- 对已有共享库做 schema sync 前，必须先确认目标库版本和 baseline 对齐方式

## 流程参考

- `references/HA-ROADMAP.md` — Phase 1 数据库迁移设计
- `docs/data-models.md` — 数据模型文档
- `cmd/migrate/main_test.go` — 迁移 CLI 单元测试
