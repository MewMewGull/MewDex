import { useEffect, useState } from "react";

export function useBlobURL(content?: Blob, mediaType = content?.type ?? "") {
  const [source, setSource] = useState<{
    content: Blob;
    mediaType: string;
    url: string;
  }>();

  useEffect(() => {
    if (!content) return;
    const url = URL.createObjectURL(content.slice(0, content.size, mediaType));
    setSource({ content, mediaType, url });
    return () => URL.revokeObjectURL(url);
  }, [content, mediaType]);

  return source?.content === content && source?.mediaType === mediaType
    ? source.url
    : "";
}
