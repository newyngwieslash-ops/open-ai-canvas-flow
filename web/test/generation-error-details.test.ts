import { describe, expect, test } from "bun:test";
import { CONTENT_MODERATION_MESSAGE, generationErrorMessage, generationFailureMetadata } from "../src/lib/generation-error";

describe("safe upstream error details", () => {
    const detail = "非常抱歉，生成的图片可能违反了关于裸露、色情或情色内容的防护限制。请重试或修改提示语。";
    test("drops upstream detail from the card", () => {
        const message = `模型服务拒绝了请求，请检查模型和参数；上游：${detail}`;
        expect(generationErrorMessage(new Error(message))).toBe("模型服务拒绝了请求，请检查模型和参数");
    });
    test("does not replace a safe upstream message with a generic HTTP hint", () => {
        const message = `模型服务暂时不可用（HTTP 500）；上游：${detail}`;
        expect(generationErrorMessage(message)).toBe("网络异常。");
        expect(generationErrorMessage("模型服务暂时不可用（HTTP 500）")).toBe("网络异常。");
    });
    test("keeps details when moderation metadata is stored", () => {
        const message = `内容审核未通过；上游：${detail}`;
        const metadata = generationFailureMetadata(message, "test prompt");
        expect(metadata.errorDetails).toBe(CONTENT_MODERATION_MESSAGE);
        expect(metadata.generationErrorCode).toBe("sensitive_words_detected");
    });
    test("gives actionable hints for known upstream failures", () => {
        expect(generationErrorMessage(`模型服务暂时不可用（HTTP 503）；上游：model_not_found request id abc`)).toContain("更换模型");
        expect(generationErrorMessage(`模型服务拒绝请求；上游：image file too large 25MB`)).toContain("压缩图片");
    });
    test("upstream marker does not bypass URL hiding", () => {
        expect(generationErrorMessage("模型服务请求失败；上游：https://private.test")).toBe("模型服务请求失败");
    });
});

// 画布节点与后端 userFacingTaskError 使用同一套归类，避免同一失败在两处说不同的话。
describe("object storage failure classification", () => {
    test("keeps real upload failures on the storage-config hint", () => {
        expect(generationErrorMessage("参考图片上传失败：bucket denied")).toContain("上传到对象存储失败");
        expect(generationErrorMessage("作品已生成，但保存未完成：上传 OSS 失败")).toContain("上传到对象存储失败");
    });
    test("does not report a read failure as an upload failure", () => {
        const message = `读取任务参考资源失败：七牛云 Kodo 读取失败：Get "https://yingce-dev.s3.cn-south-1.qiniucs.com/a.png": 404 Not Found`;
        const result = generationErrorMessage(message);
        expect(result).toContain("读取参考素材失败");
        expect(result).not.toContain("上传");
    });
    test("names the outbound block instead of blaming storage settings", () => {
        const message = `读取任务参考资源失败：七牛云 Kodo 读取失败：Get "https://yingce-dev.s3.cn-south-1.qiniucs.com/a.png": 不允许访问本机、内网或链路本地地址`;
        const result = generationErrorMessage(message);
        expect(result).toContain("出站连接被安全策略拦截");
        expect(result).not.toContain("上传");
        expect(result).not.toContain("https://");
    });
});
