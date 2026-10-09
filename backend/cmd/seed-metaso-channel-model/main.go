package main

// 一次性修复命令：为本地 SQLite 库的系统渠道（默认 CHANNEL_000003，密塔H3）注册
// MiniMax-H3 渠道模型与默认价格档。
//
// 复用真实落库路径，避免手写 SQL 与归一逻辑漂移：
//   - app.NormalizeModelCapabilityConfigForModel 归一能力配置（与 SaveAdminChannelModel 相同入口）
//   - repository.SaveChannelModelWithPriceTiers 事务化落模型与价格档
//   - repository.SyncChannelModelNames 刷新渠道 models_json 兼容列表
//
// 不做（与后端 SaveAdminChannelModel 的差异，均不影响分镜页渠道模型链路）：
//   - logical_models 前台模型投影（分镜页走 channelId::model 渠道模型，不依赖）
//   - 进程内 route catalog 失效（内存缓存，后端重启自然失效）
//   - 管理审计事件
//
// 价格按“免费”落库（unitPriceMicrocredits=0、PriceConfigured=true）：0 表示免费模型，
// 前端 hasSystemModelPrice 判定“非负数即已配置”，真实单价待用户确认后在管理后台调整
// （与 pending-test.mdx 登记一致）。

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"yingce/backend/internal/app"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func main() {
	channelID := flag.String("channel", "CHANNEL_000003", "目标系统渠道 ID")
	modelKey := flag.String("model", "MiniMax-H3", "渠道模型键")
	protocol := flag.String("protocol", "metaso-minimax-video", "渠道模型协议")
	flag.Parse()

	db, err := database.Open(database.Config{Driver: "sqlite", DataDir: os.Getenv("CANVAS_BACKEND_DATA_DIR")})
	if err != nil {
		log.Fatal(err)
	}
	if err := database.ConfigurePool(db); err != nil {
		log.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		log.Fatal(err)
	}
	repo := repository.New(db)

	channel, err := repo.SystemChannel(*channelID)
	if err != nil {
		log.Fatalf("读取系统渠道 %s 失败：%v", *channelID, err)
	}
	if channel.BaseURL != "https://metaso.cn/api/minimax" {
		log.Fatalf("渠道 %s Base URL 为 %q，与预期不符；请先人工确认再运行", *channelID, channel.BaseURL)
	}

	conflict, err := repo.ChannelModelByKeyIncludingDisabled(*channelID, *modelKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && err.Error() != "record not found" {
		log.Fatalf("查询既有模型失败：%v", err)
	}
	if conflict != nil {
		fmt.Printf("渠道 %s 已存在模型 %s（id=%s enabled=%v priceConfigured=%v），无需重复注册\n", *channelID, *modelKey, conflict.ID, conflict.Enabled, conflict.PriceConfigured)
		return
	}

	// 与 SaveAdminChannelModel 语义一致：管理员未传 capabilityConfig 时由
	// DefaultModelCapabilityConfigForModel 生成协议默认值再归一（归一函数本身不接受 nil）。
	defaultConfig := app.DefaultModelCapabilityConfigForModel(*protocol, *modelKey)
	capabilityConfig, err := app.NormalizeModelCapabilityConfigForModel("video", *protocol, *modelKey, defaultConfig)
	if err != nil {
		log.Fatalf("归一能力配置失败：%v", err)
	}
	// 与 SaveAdminChannelModel 相同：直接 json.Marshal 归一后的能力配置。
	encodedConfigBytes, err := json.Marshal(capabilityConfig)
	if err != nil {
		log.Fatalf("编码能力配置失败：%v", err)
	}
	encodedConfig := string(encodedConfigBytes)

	modelID, err := repo.NextPrefixedID("MODEL")
	if err != nil {
		log.Fatal(err)
	}
	tierID, err := repo.NextPrefixedID("PTIER")
	if err != nil {
		log.Fatal(err)
	}
	selector := map[string]string{"operation": "*"}
	selectorMap, selectorJSON, err := model.CanonicalSKUSelector(selector)
	if err != nil {
		log.Fatalf("归一价格档选择器失败：%v", err)
	}

	item := &model.ChannelModel{
		ID:                    modelID,
		ChannelID:             *channelID,
		ModelKey:              *modelKey,
		ProviderModelKey:      *modelKey,
		DisplayName:           "MiniMax-H3 (Metaso)",
		ChannelLabel:          "密塔H3",
		Description:           "密塔（metaso.cn）MiniMax V2 中转：480P/768P/1080P/2K，4–15 秒，六档固定比例；价格以密塔控制台为准，待确认后补充。",
		Capability:            "video",
		Protocol:              model.ChannelInterfaceType(*protocol),
		BillingMode:           "per_second",
		UnitPriceMicrocredits: 0,
		PriceConfigured:       true,
		Enabled:               true,
		PriceVersion:          1,
		CapabilityConfigJSON:  encodedConfig,
		CapabilityVersion:     1,
	}
	tiers := []model.ChannelModelPriceTier{{
		ID:               tierID,
		SelectorKey:      selectorJSON,
		SelectorJSON:     selectorJSON,
		Selector:         selectorMap,
		Resolution:       "*",
		ProviderModelKey: *modelKey,
		BillingMode:      "per_second",
		PriceConfigured:  true,
		Enabled:          true,
		PriceVersion:     1,
	}}

	if err := repo.SaveChannelModelWithPriceTiers(item, tiers); err != nil {
		log.Fatalf("落库渠道模型失败：%v", err)
	}
	if err := repo.SyncChannelModelNames(*channelID, time.Now()); err != nil {
		log.Fatalf("同步渠道模型名失败：%v", err)
	}

	synced, err := repo.SystemChannel(*channelID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("已注册：id=%s model=%s protocol=%s billing=%s priceConfigured=%v tiers=%d\n", item.ID, item.ModelKey, item.Protocol, item.BillingMode, item.PriceConfigured, len(tiers))
	fmt.Printf("渠道 models_json：%s\n", synced.ModelsJSON)
	fmt.Println("注意：运行中的后端进程持有内存路由缓存，重启后端后生效。")
}
