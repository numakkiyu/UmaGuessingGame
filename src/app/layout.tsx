import type { Metadata, Viewport } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "赛马娘弗一把",
  description: "输入一位马娘，顺着每一行线索把答案找出来。",
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" className="h-full">
      <body className="min-h-full bg-[var(--color-page)] text-[var(--color-ink)] antialiased">
        {children}
      </body>
    </html>
  );
}
