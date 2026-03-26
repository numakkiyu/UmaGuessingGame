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
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : "读取资源失败。" },
      { status: 404 },
    );
  }
}

function contentTypeFromPath(filePath: string) {
  const ext = filePath.split(".").pop()?.toLowerCase();
  if (ext === "png") return "image/png";
  if (ext === "jpg" || ext === "jpeg") return "image/jpeg";
  if (ext === "webp") return "image/webp";
  return "application/octet-stream";
}
