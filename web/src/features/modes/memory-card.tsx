import "@/i18n/memory";
import { useTranslation } from "@/i18n";
import { CheckCircle2 } from "lucide-react";
import type { MemoryCard } from "@/lib/api/memory";
import { cn } from "@/lib/utils";

/* 空 alt 防止读屏文本泄露配对答案 */
const CARD_ALT = "";

/* 只使用服务端下发的不透明 imagePath，不按角色 ID 拼接牌面路径 */
function CardFace({ src }: { src: string | null }) {
  if (!src) return null;
  return (
    <img
      src={src}
      alt={CARD_ALT}
      aria-hidden="true"
      // 尺寸由容器决定；用 object-contain 保留整张牌面
      className="size-full object-contain"
      // 牌面必须按服务端当前状态取，不能吃缓存里的旧图
      loading="eager"
      decoding="async"
    />
  );
}

/** 单张牌。触摸区 ≥44px（手机 4 列在 320 宽下每张约 60px） */
export function MemoryCardButton({
  card,
  disabled,
  onFlip,
  index,
}: {
  card: MemoryCard;
  disabled: boolean;
  onFlip: (cardId: string) => void;
  index: number;
}) {
  const {t}=useTranslation("memory");
  const faceUp = card.state !== "hidden";
  const matched = card.state === "matched";

  return (
    <button
      type="button"
      onClick={() => onFlip(card.cardId)}
      disabled={disabled || matched}
      // 牌面的可访问名只描述「第几行第几列的牌」与状态，不含任何角色名
      aria-label={t("cardAria",{row:Math.floor(index/4)+1,col:index%4+1,state:t(matched?"matched":faceUp?"flipped":"hidden")})}
      className={cn(
        "uma-flip-card relative flex aspect-square min-h-11 w-full items-center justify-center overflow-hidden",
        "rounded-[var(--radius-control-lg)] border-2 transition-[background-color,border-color,transform]",
        "duration-[var(--duration-quick)] ease-[var(--ease-primary)]",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
        matched
          ? "border-[var(--color-correct-edge)] bg-[var(--color-brand-green-soft)]"
          : faceUp
            ? "border-[var(--color-blue-deep)] bg-[var(--color-panel)]"
            : "border-[var(--color-line-strong)] bg-[var(--color-panel-soft)] hover:bg-[var(--color-page-tint)]",
        // 按下反馈不挪动布局
        !disabled && !matched && "motion-safe:active:scale-[0.97]",
      )}
    >
      {/* 两面保持挂载并用 backface-visibility 翻转；背面不含角色图，减少动态时直接显示终态 */}
      <span
        aria-hidden="true"
        className={cn(
          "uma-flip-card__face uma-flip-card__face--back",
          faceUp && "uma-flip-card__face--turned",
        )}
      >
        {/* 盖牌：纯色背面 + 品牌纹样，不放任何可推断信息 */}
        <span className="flex flex-col items-center gap-0.5">
          <span className="block size-4 rounded-full border-2 border-[var(--color-line-strong)]" />
          <span className="block h-0.5 w-5 rounded-full bg-[var(--color-line-strong)]" />
        </span>
      </span>
      <span
        aria-hidden="true"
        className={cn(
          "uma-flip-card__face uma-flip-card__face--front",
          faceUp && "uma-flip-card__face--turned",
        )}
      >
        {faceUp ? (
          <span className="flex size-full items-center justify-center p-1">
            <CardFace src={card.imagePath} />
          </span>
        ) : null}
      </span>

      {matched ? (
        <span
          aria-hidden="true"
          className="absolute right-1 top-1 inline-flex size-4 items-center justify-center rounded-full bg-[var(--color-correct-edge)] text-white"
        >
          <CheckCircle2 className="size-3" />
        </span>
      ) : null}
    </button>
  );
}
