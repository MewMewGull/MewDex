import { t } from "./localization.ts";
export const textPreviewEncodings = [
  {
    value: "auto",
    get label() {
      return t("自动");
    },
  },
  { value: "utf-8", label: "UTF-8" },
  { value: "utf-16le", label: "UTF-16 LE" },
  { value: "utf-16be", label: "UTF-16 BE" },
  { value: "gb18030", label: "GB18030 / GBK" },
] as const;

export type TextPreviewEncoding =
  (typeof textPreviewEncodings)[number]["value"];

export function decodeTextPreview(
  bytes: Uint8Array,
  encoding: TextPreviewEncoding,
  truncated: boolean,
) {
  const resolved =
    encoding === "auto"
      ? bytes[0] === 0xff && bytes[1] === 0xfe
        ? "utf-16le"
        : bytes[0] === 0xfe && bytes[1] === 0xff
          ? "utf-16be"
          : "utf-8"
      : encoding;
  return {
    encoding: resolved,
    text: new TextDecoder(resolved).decode(bytes, { stream: truncated }),
  };
}
