"""Strict contract and fail-closed receipt journal; no execution or transport."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import re
import sqlite3
from typing import Callable
from uuid import UUID, uuid4

LABEL = "SIMULATED — NO REAL AGENT EXECUTION"
ID = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}\Z")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
OPERATIONS = frozenset({"read_draft", "write_draft", "submit_task", "read_run",
                        "cancel_run", "read_pr"})


def canonical(value: object) -> str:
    """Contract-v1 encoding: sorted JSON, UTF-8, no whitespace or NaN."""
    return json.dumps(value, sort_keys=True, ensure_ascii=False,
                      separators=(",", ":"), allow_nan=False)


def digest(value: object) -> str:
    return "sha256:" + hashlib.sha256(canonical(value).encode("utf-8")).hexdigest()


class Rejected(Exception):
    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


class AuditUnavailable(Exception):
    """No effects committed; no durable bridge receipt is available."""


def require(condition: bool, code: str = "INVALID_ARGUMENT") -> None:
    if not condition:
        raise Rejected(code)


def identifier(value: object) -> bool:
    return isinstance(value, str) and ID.fullmatch(value) is not None


def revision(value: object) -> bool:
    return isinstance(value, str) and DIGEST.fullmatch(value) is not None


def fields(value: object, required: set[str], optional: set[str] = frozenset()) -> dict:
    require(type(value) is dict)
    require(required <= value.keys() <= required | optional)
    return value


def validate_request(value: object) -> dict:
    request = fields(value, {"contract_version", "request_id", "operation",
                             "project_id", "arguments"})
    require(request["contract_version"] == "1")
    try:
        require(str(UUID(request["request_id"])) == request["request_id"])
    except (ValueError, TypeError, AttributeError):
        raise Rejected("INVALID_ARGUMENT") from None
    require(identifier(request["project_id"]))
    require(isinstance(request["operation"], str))
    require(request["operation"] in OPERATIONS)
    require(type(request["arguments"]) is dict)
    # The first slice implements only read_draft. Other operations fail closed.
    if request["operation"] != "read_draft":
        raise Rejected("OPERATION_NOT_IMPLEMENTED")
    args = fields(request["arguments"], {"document_id"}, {"expected_revision"})
    require(identifier(args["document_id"]))
    if "expected_revision" in args:
        require(revision(args["expected_revision"]))
    return request


@dataclass(frozen=True)
class Identity:
    """Trusted host context. Never constructed from public request fields."""
    principal_id: str
    environment: str


@dataclass(frozen=True)
class Grant:
    principal_id: str
    environment: str
    project_id: str
    operations: frozenset[str]
    document_ids: frozenset[str]
    policy_revision: str
    expires_at: datetime
    enabled: bool = True

    def __post_init__(self):
        if (self.environment != "personal" or not identifier(self.project_id)
                or not self.principal_id or not revision(self.policy_revision)
                or self.expires_at.tzinfo is None
                or not self.operations <= OPERATIONS
                or not all(identifier(x) for x in self.document_ids)):
            raise ValueError("Invalid synthetic host grant")


class Journal:
    """Host-owned SQLite store. Caller must protect its directory and backups."""
    def __init__(self, path: str):
        self.connection = sqlite3.connect(path, isolation_level=None)
        self.connection.execute("PRAGMA synchronous=FULL")
        self.connection.execute("PRAGMA foreign_keys=ON")
        self.connection.executescript("""
            CREATE TABLE IF NOT EXISTS receipts (
                sequence INTEGER PRIMARY KEY AUTOINCREMENT,
                receipt TEXT NOT NULL
            );
        """)

    def close(self) -> None:
        self.connection.close()

    def record(self, receipt: dict) -> dict:
        try:
            self.connection.execute("BEGIN IMMEDIATE")
            cursor = self.connection.execute(
                "INSERT INTO receipts(receipt) VALUES (?)", (canonical(receipt),))
            receipt = {**receipt, "journal_sequence": cursor.lastrowid}
            self.connection.execute("UPDATE receipts SET receipt=? WHERE sequence=?",
                                    (canonical(receipt), cursor.lastrowid))
            self.connection.execute("COMMIT")
            return receipt
        except (sqlite3.Error, ValueError, UnicodeError):
            try:
                self.connection.execute("ROLLBACK")
            except sqlite3.Error:
                pass
            raise AuditUnavailable("AUDIT_UNAVAILABLE: receipt unavailable") from None


class Broker:
    """In-process synthetic read fixture; intentionally has no network listener.

    Registry and export checker are host-supplied, outside the request schema.
    Export defaults to deny. Fixture text is returned as untrusted data, never
    interpreted as instructions or actions.
    """
    def __init__(self, journal: Journal, grant: Grant, documents: dict[str, str], *,
                 clock: Callable[[], datetime] = lambda: datetime.now(timezone.utc),
                 export_check: Callable[[str], bool] = lambda text: False):
        if not documents.keys() <= grant.document_ids:
            raise ValueError("Fixture contains unregistered document")
        self.journal = journal
        self.grant = grant
        self._documents = dict(documents)
        self.clock = clock
        self.export_check = export_check

    def dispatch(self, request: object, identity: Identity | None) -> dict:
        now = self.clock()
        # Hash only JSON-compatible requests. Never retain rejected raw text.
        try:
            input_digest = digest(request)
        except (ValueError, TypeError, UnicodeError):
            input_digest = None
        raw = request if type(request) is dict else {}
        # Invalid caller fields are omitted from audit metadata, not echoed.
        request_id = raw.get("request_id")
        try:
            if str(UUID(request_id)) != request_id:
                request_id = None
        except (ValueError, TypeError, AttributeError):
            request_id = None
        receipt = {
            "receipt_id": str(uuid4()), "request_id": request_id,
            "operation": raw.get("operation") if isinstance(raw.get("operation"), str)
                         and raw.get("operation") in OPERATIONS
                         else None,
            "principal_id": identity.principal_id if identity else None,
            "environment": identity.environment if identity else None,
            "project_id": raw.get("project_id") if identifier(raw.get("project_id"))
                          else None,
            "policy_revision": self.grant.policy_revision,
            "input_digest": input_digest, "recorded_at": now.isoformat(),
            "simulated": True,
        }
        data = None
        try:
            require(identity is not None, "UNAUTHENTICATED")
            # Check host identity before parsing resource selectors.
            require(identity.principal_id == self.grant.principal_id and
                    identity.environment == self.grant.environment, "DENIED")
            require(self.grant.enabled, "PROJECT_DISABLED")
            require(now < self.grant.expires_at, "DENIED")
            parsed = validate_request(request)
            require(parsed["project_id"] == self.grant.project_id, "DENIED")
            require(parsed["operation"] in self.grant.operations, "DENIED")
            args = parsed["arguments"]
            document_id = args["document_id"]
            require(document_id in self.grant.document_ids, "DENIED")
            require(document_id in self._documents, "NOT_FOUND")
            text = self._documents[document_id]
            try:
                size = len(text.encode("utf-8"))
            except UnicodeError:
                raise Rejected("INVALID_ARGUMENT") from None
            require(size <= 128 * 1024, "LIMIT_EXCEEDED")
            content_revision = "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()
            if "expected_revision" in args:
                require(args["expected_revision"] == content_revision, "REVISION_MISMATCH")
            require(self.export_check(text) is True, "EXPORT_BLOCKED")
            data = {"summary": LABEL, "text": text, "revision": content_revision,
                    "byte_count": size, "document_id": document_id,
                    "provenance": "host synthetic fixture; untrusted data",
                    "simulated": True}
            receipt.update(decision="allowed", effect="draft_read", outcome="completed",
                           document_id=document_id, document_revision=content_revision,
                           output_digest=content_revision, exported_bytes=size)
        except Rejected as error:
            receipt.update(decision="denied", effect="none", outcome="rejected",
                           error_code=error.code)
        except Exception:
            # Do not export provider/validator exception messages or fixture bytes.
            data = None
            receipt.update(decision="denied", effect="none", outcome="rejected",
                           error_code="RESOURCE_UNAVAILABLE")
        # Nothing is returned if durable audit commit fails, including reads.
        return {"summary": LABEL, "simulated": True,
                "receipt": self.journal.record(receipt), "data": data}
