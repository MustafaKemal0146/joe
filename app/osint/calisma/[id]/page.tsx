import { OsintCalismaView } from "@/components/views/osint-calisma-view";

export default async function OsintCalismaPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <OsintCalismaView runId={id} />;
}
