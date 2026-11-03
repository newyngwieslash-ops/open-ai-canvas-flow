# Spec Delta

## Purpose

为缺乏可信身份或停写证据的旧资源建立可核验、可审计的维护判定流程，先在隔离副本证明接管安全性，再按精确对象和操作授权维护；未知历史变体、共享归属或旧写者未决时继续阻断，不默认修补业务状态。

## ADDED Requirements

### Requirement: 可信身份及历史停写来源
系统 SHALL 仅接受来源可核对的manifest及历史物理身份、归属、变体/alias和全部writer停写证据，不以当前配置、文件相似或自报trusted代替。

#### Scenario: 未知旧缩略图或缺少writer证据
- **WHEN** 无法证明必要变体位置、归属或历史写者可靠终态
- **THEN** 判为blocked，保留原记录和物理对象，不猜key、不补settled

### Requirement: 隔离dry-run与实际入口
系统 SHALL 先在授权隔离副本产生逐对象eligible/blocked/conflict、依据和预计差异，再以实际入口验证失败恢复与重复执行。

#### Scenario: 过期快照及共享归属
- **WHEN** 快照已变化、跨用户/namespace或存在同物理alias
- **THEN** 阻断或重取授权证据，不修改其他归属对象；隔离执行失败不清业务记录或伪造结算

### Requirement: 真实维护逐项授权
系统 SHALL 在审阅精确差异、对象清单、备份/恢复、窗口和写入屏障后才执行批准操作；只读、补登记、真实删除、停服务及迁移 MUST 分别明确。

#### Scenario: 只有提案或dry-run批准
- **WHEN** 尚无具体live维护授权
- **THEN** 不读取或写入原业务库、不删真实对象、不控制既有服务；继续提供隔离报告并保留未决项
