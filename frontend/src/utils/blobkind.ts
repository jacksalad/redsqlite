/** 常见图片格式识别（V2.5 BLOB 增强）：从 Base64 前缀读魔数。 */

export interface ImageKind {
  mime: string;
  ext: string;
  label: string;
}

/** 识别 Base64 数据的图片类型；非图片或无法解码返回 null。 */
export function detectImageKind(b64: string): ImageKind | null {
  if (!b64) return null;
  let prefix = "";
  try {
    prefix = atob(b64.slice(0, 32));
  } catch {
    return null;
  }
  const b = (i: number) => prefix.charCodeAt(i);
  if (prefix.startsWith("\x89PNG")) return { mime: "image/png", ext: "png", label: "PNG" };
  if (b(0) === 0xff && b(1) === 0xd8 && b(2) === 0xff) return { mime: "image/jpeg", ext: "jpg", label: "JPEG" };
  if (prefix.startsWith("GIF8")) return { mime: "image/gif", ext: "gif", label: "GIF" };
  if (prefix.startsWith("BM")) return { mime: "image/bmp", ext: "bmp", label: "BMP" };
  if (prefix.startsWith("RIFF") && prefix.slice(8, 12) === "WEBP") {
    return { mime: "image/webp", ext: "webp", label: "WebP" };
  }
  if (b(0) === 0 && b(1) === 0 && b(2) === 1 && b(3) === 0) {
    return { mime: "image/x-icon", ext: "ico", label: "ICO" };
  }
  return null;
}
