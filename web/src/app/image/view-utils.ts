import type {
  ImageConversation,
  StoredImage,
  StoredSourceImage,
} from "@/store/image-conversations";
import webConfig from "@/constants/common-env";
import { getCachedAuthKey } from "@/store/auth";

function normalizeImageURL(url?: string) {
  const trimmed = String(url || "").trim();
  if (!trimmed) {
    return "";
  }
  if (/^data:/i.test(trimmed)) {
    return trimmed;
  }
  const base = webConfig.apiUrl.replace(/\/$/, "");
  const normalized = /^https?:\/\//i.test(trimmed)
    ? trimmed
    : `${base}${trimmed.startsWith("/") ? trimmed : `/${trimmed}`}`;
  return appendImageAuthToken(normalized);
}

function appendImageAuthToken(url: string) {
  if (!url.includes("/v1/files/image/")) {
    return url;
  }
  const token = getCachedAuthKey();
  if (!token) {
    return url;
  }
  try {
    const parsed = new URL(
      url,
      typeof window === "undefined" ? webConfig.apiUrl : window.location.href,
    );
    if (!parsed.pathname.includes("/v1/files/image/")) {
      return url;
    }
    parsed.searchParams.set("image_token", token);
    return parsed.toString();
  } catch {
    const separator = url.includes("?") ? "&" : "?";
    return `${url}${separator}image_token=${encodeURIComponent(token)}`;
  }
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
    return normalizeImageURL(
      normalized.replace("/v1/files/image/", "/v1/files/image-thumb/"),
    );
  }
  return normalizeImageURL(normalized);
}

export function buildSourceImageUrl(source: StoredSourceImage) {
  return normalizeImageURL(source.dataUrl || source.url);
}

export function buildSourceImageThumbnailUrl(source: StoredSourceImage) {
  return source.dataUrl
    ? normalizeImageURL(source.dataUrl)
    : buildThumbnailURL(source.url || "");
}

export function buildConversationSourceLabel(source: StoredSourceImage) {
  return source.role === "mask" ? "选区 / 遮罩" : "源图";
}

export function buildConversationPreviewSource(
  conversation: ImageConversation,
) {
  const latestSuccessfulImage = conversation.images.find(
    (image) => image.status === "success" && (image.b64_json || image.url),
  );
  if (latestSuccessfulImage) {
    return buildImageThumbnailUrl(latestSuccessfulImage);
  }

  const firstSourceImage = conversation.sourceImages?.find(
    (item) => item.role === "image",
  );
  return firstSourceImage ? buildSourceImageThumbnailUrl(firstSourceImage) : "";
}
