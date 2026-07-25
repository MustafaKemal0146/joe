"""initial

Revision ID: f88ce5376567
Revises: 
Create Date: 2026-07-20 21:42:02.262055

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = 'f88ce5376567'
down_revision: Union[str, Sequence[str], None] = None
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Create the base tables used by the later feature migrations."""
    from app.db import Base
    import app.models  # noqa: F401

    table_names = {
        "cases",
        "provider_connections",
        "artifacts",
        "evidence_items",
        "analysis_sessions",
        "council_turns",
        "osint_runs",
    }
    Base.metadata.create_all(
        bind=op.get_bind(),
        tables=[table for name, table in Base.metadata.tables.items() if name in table_names],
    )


def downgrade() -> None:
    from app.db import Base
    import app.models  # noqa: F401

    for name in (
        "council_turns",
        "evidence_items",
        "analysis_sessions",
        "osint_runs",
        "artifacts",
        "provider_connections",
        "cases",
    ):
        table = Base.metadata.tables.get(name)
        if table is not None:
            table.drop(op.get_bind(), checkfirst=True)
