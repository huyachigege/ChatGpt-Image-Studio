import type { ImageConversation, StoredImage, StoredSourceImage } from "@/store/image-conversations";
import webConfig from "@/constants/common-env";

function normalizeImageURL(url?: string) {
  const trimmed = String(url || "").trim();
  if (!trimmed) {
    return "";
  }
  if (/^(data:|https?:\/\/)/i.test(trimmed)) {
    return trimmed;
  }
  const base = webConfig.apiUrl.replace(/\/$/, "");
  return `${base}${trimmed.startsWith("/") ? trimmed : `/${trimmed}`}`;
}

export function buildImageDataUrl(image: StoredImage) {
  if (image.url) {
    return normalizeImageURL(image.url);
  }
  if (!image.b64_json) {
    return "";
  }
  return `data:image/png;base64,${image.b64_json}`;
}

export function buildImageThumbnailUrl(image: StoredImage) {
  const url = String(image.url || "").trim();
  if (!url) {
    return buildImageDataUrl(image);
  }
  return buildThumbnailURL(url);
}

function buildThumbnailURL(url: string) {
  const normalized = String(url || "").trim();
  if (!normalized) {
    return "";
  }
  if (normalized.includes("/v1/files/image/")) {
    return normalizeImageURL(normalized.replace("/v1/files/image/", "/v1/files/image-thumb/"));
  }
  return normalizeImageURL(normalized);
}

export function buildSourceImageUrl(source: StoredSourceImage) {
  return normalizeImageURL(source.dataUrl || source.url);
}

export function buildSourceImageThumbnailUrl(source: StoredSourceImage) {
  return source.dataUrl ? normalizeImageURL(source.dataUrl) : buildThumbnailURL(source.url || "");
}

export function buildConversationSourceLabel(source: StoredSourceImage) {
  return source.role === "mask" ? "选区 / 遮罩" : "源图";
}

export function buildConversationPreviewSource(conversation: ImageConversation) {
  const latestSuccessfulImage = conversation.images.find(
    (image) => image.status === "success" && (image.b64_json || image.url),
  );
  if (latestSuccessfulImage) {
    return buildImageThumbnailUrl(latestSuccessfulImage);
  }

  const firstSourceImage = conversation.sourceImages?.find((item) => item.role === "image");
  return firstSourceImage ? buildSourceImageThumbnailUrl(firstSourceImage) : "";
}
