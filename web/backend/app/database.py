from sqlalchemy import create_engine, inspect, text
from sqlalchemy.orm import DeclarativeBase, sessionmaker

from .config import settings

engine = create_engine(
    settings.DATABASE_URL,
    connect_args={"check_same_thread": False},  # SQLite
)
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)


class Base(DeclarativeBase):
    pass


def init_db():
    try:
        Base.metadata.create_all(bind=engine, checkfirst=True)
    except Exception:
        # Handle race condition when multiple workers start simultaneously
        pass
    migrate()


def migrate():
    """Add columns that create_all() cannot add to pre-existing tables.

    create_all() only creates missing *tables*, so a database from an older
    deploy keeps its old schema and every query for a new column fails with
    "no such column". Each step is idempotent — safe to run on every startup.
    """
    from . import models  # noqa: F401  (ensure metadata is populated)

    inspector = inspect(engine)
    existing_tables = set(inspector.get_table_names())

    with engine.begin() as conn:
        for table in Base.metadata.sorted_tables:
            if table.name not in existing_tables:
                continue
            present = {col["name"] for col in inspector.get_columns(table.name)}
            for column in table.columns:
                if column.name in present:
                    continue
                ddl = f'ALTER TABLE "{table.name}" ADD COLUMN "{column.name}" {column.type.compile(engine.dialect)}'
                # SQLite refuses NOT NULL without a default on ADD COLUMN, and
                # every column added so far is nullable, so keep it simple.
                conn.execute(text(ddl))
