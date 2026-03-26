import { NextResponse } from "next/server";
import { getPublicConfig } from "@/config/public";

export async function GET() {
  return NextResponse.json(getPublicConfig());
}
