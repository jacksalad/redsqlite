/** 统一的错误消息提取（文档 12）。 */
export function getErrorMessage(error: unknown): string {
  if (error === null || error === undefined) {
    return "未知错误";
  }
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  if (typeof error === "object") {
    const anyErr = error as { message?: unknown; data?: unknown };
    if (typeof anyErr.message === "string") {
      return anyErr.message;
    }
    if (typeof anyErr.data === "string") {
      return anyErr.data;
    }
  }
  return String(error);
}
