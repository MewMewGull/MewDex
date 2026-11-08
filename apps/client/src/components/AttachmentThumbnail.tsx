import { useState } from "react";
import { Image as ImageIcon, LoaderCircle } from "lucide-react";
import { t } from "../localization.ts";
import { useBlobURL } from "../useBlobURL";

export function AttachmentThumbnail({
  content,
  mediaType,
  name,
  failed = false,
}: {
  content?: Blob;
  mediaType: string;
  name: string;
  failed?: boolean;
}) {
  const url = useBlobURL(content, mediaType);
  const [failedURL, setFailedURL] = useState("");
  const unavailable = failed || (!!url && failedURL === url);

  return (
    <span className="attachment-thumbnail">
      {url && !unavailable ? (
        <img src={url} alt={name} onError={() => setFailedURL(url)} />
      ) : (
        <>
          {unavailable ? (
            <ImageIcon size={24} aria-hidden="true" />
          ) : (
            <LoaderCircle size={22} className="spinning" aria-hidden="true" />
          )}
          <span className="sr-only">
            {unavailable
              ? t("无法预览这张图片，可以保存到本机查看。")
              : t("正在读取附件…")}
          </span>
        </>
      )}
    </span>
  );
}
