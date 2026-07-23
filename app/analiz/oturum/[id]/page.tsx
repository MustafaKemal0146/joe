import { AnalizOturumView } from "@/components/views/analiz-oturum-view";

export default async function AnalizOturumPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <AnalizOturumView analysisId={id} />;
}
