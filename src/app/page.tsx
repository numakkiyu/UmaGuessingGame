import { GameHeader } from "@/components/game-header";
import { SiteFooter } from "@/components/site-footer";
import { getPublicConfig } from "@/config/public";
import { loadQuestionBankReady } from "@/lib/question-bank";

export default async function Home() {
  const questionBank = await loadQuestionBankReady();
  const config = getPublicConfig();

  return (
    <main className="relative mx-auto flex min-h-screen w-full max-w-[1320px] flex-col px-4 py-4 sm:px-6 lg:px-8">
      <div className="pointer-events-none absolute inset-x-6 top-0 -z-10 h-64 rounded-b-[52px] bg-[linear-gradient(180deg,rgba(239,209,172,0.4),rgba(245,239,226,0))]" />
      <div className="pointer-events-none absolute left-0 right-0 top-20 -z-10 h-px bg-[linear-gradient(90deg,rgba(156,69,24,0),rgba(156,69,24,0.24),rgba(156,69,24,0))]" />

      <GameHeader
        questionBankSize={questionBank.length}
        maxGuesses={config.maxGuesses}
        turnstileEnabled={config.turnstileEnabled}
        turnstileSiteKey={config.turnstileSiteKey}
      />
      <SiteFooter className="mt-5" />
    </main>
  );
}
