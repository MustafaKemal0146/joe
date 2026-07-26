import { AnalizOturumView } from "@/components/views/analiz-oturum-view";
import { analysisIdFromRoute } from "@/lib/analysis-path";

export default async function AnalizOturumPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <AnalizOturumView analysisId={analysisIdFromRoute(id)} />;
}
