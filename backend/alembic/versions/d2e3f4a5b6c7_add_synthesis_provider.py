"""add explicit synthesis provider

Revision ID: d2e3f4a5b6c7
Revises: c1d2e3f4a5b6
Create Date: 2026-07-26
"""

from alembic import op
import sqlalchemy as sa


revision = "d2e3f4a5b6c7"
down_revision = "c1d2e3f4a5b6"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "analysis_sessions",
        sa.Column("synthesis_provider_connection_id", sa.String(length=36), nullable=True),
    )
    op.create_foreign_key(
        "fk_analysis_sessions_synthesis_provider",
        "analysis_sessions",
        "provider_connections",
        ["synthesis_provider_connection_id"],
        ["id"],
        ondelete="SET NULL",
    )


def downgrade() -> None:
    op.drop_constraint("fk_analysis_sessions_synthesis_provider", "analysis_sessions", type_="foreignkey")
    op.drop_column("analysis_sessions", "synthesis_provider_connection_id")
