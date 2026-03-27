import { NextResponse } from "next/server";
import { readAssetBytes } from "@/lib/assets/service";

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ assetId: string }> },
  ) {
  try {
    const { assetId } = await params;
    const { entry, bytes } = await readAssetBytes(assetId);
    return new NextResponse(bytes, {
      headers: {
        "Cache-Control": "public, max-age=3600, stale-while-revalidate=86400",
        "Content-Type": contentTypeFromPath(entry.local_path),
      },
    });
  } catch {
    return new NextResponse(null, {
      status: 404,
      headers: {
        "Cache-Control": "no-store",
      },
    });
  }
}

function contentTypeFromPath(filePath: string) {
  const ext = filePath.split(".").pop()?.toLowerCase();
  if (ext === "png") return "image/png";
  if (ext === "jpg" || ext === "jpeg") return "image/jpeg";
  if (ext === "webp") return "image/webp";
  if (ext === "svg") return "image/svg+xml; charset=utf-8";
  return "application/octet-stream";
}
