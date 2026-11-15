"""Check the local backend demo through its HTTP APIs."""

import json
import time
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen
from uuid import uuid4


REQUEST_TIMEOUT = 5
POLL_TIMEOUT = 30


def _request(base_url, method, path, token, *, expect=200, payload=None):
    headers = {"Accept": "application/json", "Authorization": f"Bearer {token}"}
    data = None
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = Request(base_url.rstrip("/") + path, data=data, headers=headers, method=method)
    label = f"{method} {path}"
    try:
        with urlopen(request, timeout=REQUEST_TIMEOUT) as response:
            if response.status != expect:
                raise RuntimeError(f"{label}: expected HTTP {expect}, got HTTP {response.status}")
            result = json.load(response)
    except HTTPError as error:
        error.close()
        if error.code == expect:
            return None
        raise RuntimeError(f"{label}: expected HTTP {expect}, got HTTP {error.code}") from None
    except (URLError, OSError):
        raise RuntimeError(f"{label}: could not reach the service or the request timed out") from None
    except ValueError:
        raise RuntimeError(f"{label}: expected a JSON response") from None

    if not isinstance(result, dict):
        raise RuntimeError(f"{label}: expected a JSON object")
    return result


def _required_id(value, field, label):
    identifier = value.get(field)
    if not isinstance(identifier, str) or not identifier.strip():
        raise RuntimeError(f"{label}: missing {field}")
    return identifier


def _wait_for_operation(api_url, path, token, expected_status):
    deadline = time.monotonic() + POLL_TIMEOUT
    while time.monotonic() < deadline:
        operation = _request(api_url, "GET", path, token)
        if operation.get("delivery_state") == "delivered" and operation.get("service_status") == expected_status:
            return operation
        if operation.get("needs_manual_check"):
            raise RuntimeError(f"{path}: automatic reconciliation was exhausted")
        time.sleep(0.25)
    raise RuntimeError(f"Operation did not reach delivered / {expected_status} within {POLL_TIMEOUT} seconds")


def _apply_decision(api_url, mock_url, operation_path, external_id, session_token, decision_token, status):
    decision_path = f"/requests/{quote(external_id, safe='')}/decision"
    decision = _request(mock_url, "PATCH", decision_path, decision_token, payload={"status": status})
    if decision.get("id") != external_id or decision.get("status") != status:
        raise RuntimeError(f"Mock service did not save the {status} decision")
    result = _wait_for_operation(api_url, operation_path, session_token, status)
    if result.get("external_id") != external_id:
        raise RuntimeError(f"{status} operation returned a different external request ID")
    return result


def _verify_rejection(api_url, mock_url, atlas_token, orbit_token, decision_token):
    conversation = _request(api_url, "POST", "/api/conversations", orbit_token, expect=201)
    conversation_id = _required_id(conversation, "id", "Create Orbit conversation")
    reply = _request(api_url, "POST", f"/api/conversations/{quote(conversation_id, safe='')}/messages",
                     orbit_token, payload={"client_message_id": str(uuid4()), "text": "Нужен доступ к учебному GitLab"})
    draft = reply.get("draft")
    if not isinstance(draft, dict) or draft.get("resource_id") != "orbit-training-repo":
        raise RuntimeError("Orbit did not receive a draft for its own repository")
    draft_path = f"/api/drafts/{quote(_required_id(draft, 'id', 'Orbit draft'), safe='')}"
    edited = _request(api_url, "PATCH", draft_path, orbit_token, payload={
        "expected_version": draft["version"], "justification": "Нужно прочитать учебный README.",
    })
    operation = _request(api_url, "POST", f"{draft_path}/confirm", orbit_token, payload={"version": edited["version"]})
    operation_id = _required_id(operation, "id", "Confirm Orbit draft")
    operation_path = f"/api/operations/{quote(operation_id, safe='')}"
    pending = _wait_for_operation(api_url, operation_path, orbit_token, "pending_approval")
    external_id = _required_id(pending, "external_id", "Delivered Orbit operation")
    _request(api_url, "GET", operation_path, atlas_token, expect=404)
    rejected = _apply_decision(api_url, mock_url, operation_path, external_id, orbit_token, decision_token, "rejected")
    if "наставника" not in rejected.get("next_step", ""):
        raise RuntimeError("Rejected request did not provide a recovery step")
    print("Operator rejection is visible through the backend API. Atlas access denied.")


def smoke(api_url, mock_url, atlas_token, orbit_token, decision_token, *, restart=None) -> None:
    """Verify approval and rejection through the demo's HTTP APIs."""
    with urlopen(api_url, timeout=REQUEST_TIMEOUT) as page:
        if b'<title>AI-buddy Workspace</title>' not in page.read():
            raise RuntimeError("The web frontend did not serve AI-buddy")
    for team, token in (("atlas", atlas_token), ("orbit", orbit_token)):
        profile = _request(api_url, "GET", "/api/me", token)
        if profile.get("id") != f"{team}-intern" or profile.get("team_id") != team or profile.get("role") != "intern":
            raise RuntimeError(f"GET /api/me: unexpected profile for the {team} demo session")
    print("Atlas and Orbit sessions verified.")

    document_path = "/api/documents/atlas-repo"
    document = _request(api_url, "GET", document_path, atlas_token)
    if document.get("document_id") != "atlas-repo" or not document.get("chunks"):
        raise RuntimeError("Atlas document lookup returned no chunks")
    _request(api_url, "GET", document_path, orbit_token, expect=404)
    for team, other, token in (("atlas", "orbit", atlas_token), ("orbit", "atlas", orbit_token)):
        library = _request(api_url, "GET", "/api/documents", token)
        ids = [document["id"] for document in library["documents"]]
        if f"{team}-repo" not in ids or f"{other}-repo" in ids or len(ids) != len(set(ids)):
            raise RuntimeError(f"Document library returned incorrect visibility or duplicate entries for {team}")
    print("Atlas document access verified for both teams.")

    examples = _request(api_url, "POST", "/api/conversations", atlas_token, expect=201)
    examples_id = _required_id(examples, "id", "Create examples conversation")
    examples_path = f"/api/conversations/{quote(examples_id, safe='')}/messages"
    for question, expected_document in (
        ("Как мне написать руководителю?", None),
        ("Где смотреть задачи команды в Jira?", "atlas-tracker"),
        ("Как подключиться к учебному VPN?", "atlas-vpn"),
    ):
        reply = _request(api_url, "POST", examples_path, atlas_token,
                         payload={"client_message_id": str(uuid4()), "text": question})
        documents = [source.get("document_id") for source in reply.get("sources", [])]
        if expected_document is None:
            if documents or "нет ответа на этот вопрос" not in reply.get("answer", ""):
                raise RuntimeError("An unsupported question did not receive the fallback")
        elif expected_document not in documents:
            raise RuntimeError(f"Example question did not cite {expected_document}")
    print("Unsupported question, Jira and VPN examples verified.")

    conversation = _request(api_url, "POST", "/api/conversations", atlas_token, expect=201)
    conversation_id = _required_id(conversation, "id", "Create conversation")
    message_path = f"/api/conversations/{quote(conversation_id, safe='')}/messages"
    answer = _request(
        api_url,
        "POST",
        message_path,
        atlas_token,
        payload={"client_message_id": str(uuid4()), "text": "Нужен доступ к учебному GitLab"},
    )
    if not any(source.get("document_id") == "atlas-repo" for source in answer.get("sources", [])):
        raise RuntimeError("GitLab response did not cite the Atlas document from PostgreSQL")
    conversation_path = f"/api/conversations/{quote(conversation_id, safe='')}"
    history = _request(api_url, "GET", conversation_path, atlas_token)
    if [entry.get("role") for entry in history.get("messages", [])] != ["user", "assistant"]:
        raise RuntimeError("Conversation did not retain the question and answer")
    _request(api_url, "GET", conversation_path, orbit_token, expect=404)
    role = _request(
        api_url, "POST", message_path, atlas_token,
        payload={"client_message_id": str(uuid4()), "text": "А какая роль нужна?"},
    )
    role_documents = {source.get("document_id") for source in role.get("sources", [])}
    if ("Reporter" not in role.get("answer", "") or not role_documents
            or not role_documents <= {"atlas-repo", "atlas-first-task"}):
        raise RuntimeError("The follow-up about the GitLab role did not cite the team's policy")
    print("GitLab role follow-up retained the team's context.")
    draft = answer.get("draft")
    if not isinstance(draft, dict) or draft.get("tool_id") != "gitlab" or draft.get("resource_id") != "atlas-training-repo":
        raise RuntimeError("GitLab message did not create a draft for the Atlas training repository")
    if draft.get("justification") != "":
        raise RuntimeError("Draft included a justification absent from the user question")
    draft_id = _required_id(draft, "id", "Create draft")
    version = draft.get("version")
    if type(version) is not int or version < 1:
        raise RuntimeError("GitLab draft returned an invalid version")

    draft_path = f"/api/drafts/{quote(draft_id, safe='')}"
    justification = "Нужно прочитать учебный README для первого задания."
    edited = _request(
        api_url,
        "PATCH",
        draft_path,
        atlas_token,
        payload={"expected_version": version, "justification": justification},
    )
    if edited.get("id") != draft_id or edited.get("version") != version + 1 or edited.get("justification") != justification:
        raise RuntimeError("Draft update did not save the justification and increment the version")

    confirmation = {"version": edited["version"]}
    operation = _request(api_url, "POST", f"{draft_path}/confirm", atlas_token, payload=confirmation)
    operation_id = _required_id(operation, "id", "Confirm draft")
    repeated = _request(api_url, "POST", f"{draft_path}/confirm", atlas_token, payload=confirmation)
    if repeated.get("id") != operation_id:
        raise RuntimeError("Repeating draft confirmation returned a different operation")
    print("Chat cited PostgreSQL, retained history and confirmed a GitLab draft idempotently.")

    operation_path = f"/api/operations/{quote(operation_id, safe='')}"
    pending = _wait_for_operation(api_url, operation_path, atlas_token, "pending_approval")
    external_id = _required_id(pending, "external_id", "Delivered operation")
    _request(api_url, "GET", operation_path, orbit_token, expect=404)
    status = _request(
        api_url, "POST", message_path, atlas_token,
        payload={"client_message_id": str(uuid4()), "text": "Проверить статус заявки"},
    )
    if external_id not in status.get("answer", "") or "Дождитесь решения" not in status["answer"] or status.get("sources"):
        raise RuntimeError("Chat did not report the saved pending status for the confirmed request")
    _request(api_url, "POST", "/api/conversations", atlas_token, expect=201)
    atlas_requests = _request(api_url, "GET", "/api/operations", atlas_token)["operations"]
    if not any(item.get("id") == operation_id and item.get("external_id") == external_id
               and item.get("resource_id") == "atlas-training-repo" for item in atlas_requests):
        raise RuntimeError("The Atlas request list lost the confirmed request after a new conversation")
    orbit_requests = _request(api_url, "GET", "/api/operations", orbit_token)["operations"]
    if any(item.get("id") == operation_id for item in orbit_requests):
        raise RuntimeError("The Orbit request list exposed an Atlas operation")
    print("Operation delivered and pending approval. Orbit access denied.")

    _apply_decision(api_url, mock_url, operation_path, external_id, atlas_token, decision_token, "approved")
    print("Operator approval is visible through the backend API.")
    _verify_rejection(api_url, mock_url, atlas_token, orbit_token, decision_token)

    if restart is not None:
        before = _request(api_url, "GET", conversation_path, atlas_token)["messages"]
        restart()
        restored = _wait_for_operation(api_url, operation_path, atlas_token, "approved")
        history = _request(api_url, "GET", conversation_path, atlas_token)
        if restored.get("external_id") != external_id or history.get("messages") != before:
            raise RuntimeError("Restart did not preserve the request ID and conversation")
        _request(api_url, "GET", operation_path, orbit_token, expect=404)
        print("Compose restart preserved the conversation, request and access restrictions.")
