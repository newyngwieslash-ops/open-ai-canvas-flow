import { getMediaBlob } from "@/services/file-storage";
import { imageToDataUrl } from "@/services/image-storage";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";
import { isPublicMediaUrl } from "./video-validation";
import { blobToDataUrl } from "./video-response";

export async function resolveVideoReferenceImageUrl(image: ReferenceImage, provider: string) {
    const directUrl = image.url || image.dataUrl;
    if (isPublicMediaUrl(directUrl) || directUrl.startsWith("data:")) return directUrl;
    const dataUrl = await imageToDataUrl(image);
    if (!dataUrl) throw new Error(`${provider} 参考图读取失败，请换一张图片或重新上传`);
    return dataUrl;
}

export async function resolveVideoReferenceMediaUrl(media: ReferenceVideo | ReferenceAudio, label: string, signal?: AbortSignal) {
    if (isPublicMediaUrl(media.url) || media.url?.startsWith("data:")) return media.url;
    let blob: Blob | null = null;
    if (media.storageKey) blob = await getMediaBlob(media.storageKey);
    if (!blob && media.url?.startsWith("blob:")) {
        const response = await fetch(media.url, { signal });
        if (!response.ok) throw new Error(`${label}读取失败`);
        blob = await response.blob();
    }
    if (!blob) throw new Error(`${label}必须是公网 URL 或本地已保存素材`);
    return blobToDataUrl(blob);
}
