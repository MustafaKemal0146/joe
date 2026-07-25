"""add runtime integrity indexes

Revision ID: c1d2e3f4a5b6
Revises: 4b4dbd41c801
"""
from typing import Sequence, Union

from alembic import op


revision: str = "c1d2e3f4a5b6"
down_revision: Union[str, Sequence[str], None] = "4b4dbd41c801"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_index(
        "ux_osint_run_events_run_sequence",
        "osint_run_events",
        ["osint_run_id", "sequence"],
        unique=True,
    )
    op.create_index(
        "ux_analysis_events_session_sequence",
        "analysis_events",
        ["analysis_session_id", "sequence"],
        unique=True,
    )
    op.create_index(
        "ux_imported_profiles_batch_username",
        "imported_profiles",
        ["import_batch_id", "username"],
        unique=True,
    )
    op.create_index(
        "ux_imported_conversations_batch_source",
        "imported_conversations",
        ["import_batch_id", "source_conversation_id"],
        unique=True,
    )


def downgrade() -> None:
    op.drop_index("ux_imported_conversations_batch_source", table_name="imported_conversations")
    op.drop_index("ux_imported_profiles_batch_username", table_name="imported_profiles")
    op.drop_index("ux_analysis_events_session_sequence", table_name="analysis_events")
    op.drop_index("ux_osint_run_events_run_sequence", table_name="osint_run_events")
