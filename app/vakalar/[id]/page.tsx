import { CaseDetailShell } from "@/components/views/case-detail/case-detail-shell";

export default async function CaseDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <CaseDetailShell caseId={id} />;
}
