import { useEffect, useState } from "react";

/** 主题偏好（与后端 domain.Settings.Theme 一致）。 */
export type ThemePref = "system" | "light" | "dark";

/** 把主题偏好解析为实际主题并应用到 <html data-theme>。 */
export function applyTheme(pref: string): "light" | "dark" {
  let theme: "light" | "dark" = "dark";
  if (pref === "light") theme = "light";
  else if (pref === "dark") theme = "dark";
  else theme = window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  document.documentElement.dataset.theme = theme;
  return theme;
}

/** 订阅 resolved 主题（跟随 data-theme 属性变化，供 CodeMirror 等命令式 API 使用）。 */
export function useResolvedTheme(): "light" | "dark" {
  const [theme, setTheme] = useState<"light" | "dark">(
    () => (document.documentElement.dataset.theme === "light" ? "light" : "dark"),
  );
  useEffect(() => {
    const read = () =>
      setTheme(document.documentElement.dataset.theme === "light" ? "light" : "dark");
    read();
    const observer = new MutationObserver(read);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);
  return theme;
}
