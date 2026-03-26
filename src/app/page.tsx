import { GameShell } from "@/components/game-shell";
import { loadQuestionBankReady, loadSearchIndex } from "@/lib/question-bank";

export default async function Home() {
  const [questionBank, searchEntries] = await Promise.all([
    loadQuestionBankReady(),
    loadSearchIndex(),
  ]);

  return (
    <GameShell
      initialQuestionBankSize={questionBank.length}
      initialSearchEntries={searchEntries}
    />
  );
}
