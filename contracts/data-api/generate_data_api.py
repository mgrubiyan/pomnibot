#!/usr/bin/env python3
"""
generate_data_api.py

Generates a valid DATA-API.yaml file compliant with DATA-API 1.0 specification
from an OpenAPI 3.0.x / 3.1.x file.
Fully expands (dereferences) JSON schemas so that bodySchema and request bodies
are self-contained without unresolved $ref pointers.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

import yaml

PATH_PARAM_RE = re.compile(r"\{([A-Za-z0-9_.-]+)\}")
NON_ALPHANUM_RE = re.compile(r"[^A-Za-z0-9._-]+")


def sanitize_id(raw: str) -> str:
    """Sanitizes a string to fit the DATA-API id pattern: ^[A-Za-z0-9._-]+$"""
    sanitized = NON_ALPHANUM_RE.sub("-", raw).strip("-._")
    return sanitized or "check"


def resolve_ref(ref: str, root_doc: Dict[str, Any]) -> Any:
    """Resolves an internal JSON pointer like #/components/schemas/CardSet."""
    if not isinstance(ref, str) or not ref.startswith("#/"):
        return None
    parts = ref.lstrip("#/").split("/")
    cur: Any = root_doc
    for part in parts:
        part = part.replace("~1", "/").replace("~0", "~")
        if isinstance(cur, dict) and part in cur:
            cur = cur[part]
        elif isinstance(cur, list) and part.isdigit() and int(part) < len(cur):
            cur = cur[int(part)]
        else:
            return None
    return cur


def dereference_schema(
    schema: Any,
    root_doc: Dict[str, Any],
    seen: Optional[Set[str]] = None,
) -> Any:
    """
    Recursively expands all $ref occurrences in a schema or object.
    Protects against circular references.
    """
    if seen is None:
        seen = set()

    if isinstance(schema, dict):
        if "$ref" in schema:
            ref = schema["$ref"]
            if ref in seen:
                # Break circular reference gracefully
                return {"type": "object", "description": f"Circular ref to {ref}"}
            target = resolve_ref(ref, root_doc)
            if target is not None:
                new_seen = seen | {ref}
                resolved = dereference_schema(target, root_doc, new_seen)
                if isinstance(resolved, dict):
                    merged = dict(resolved)
                    # Sibling properties can override or extend
                    for k, v in schema.items():
                        if k != "$ref":
                            merged[k] = dereference_schema(v, root_doc, new_seen)
                    return merged
                return resolved
            return schema

        res: Dict[str, Any] = {}
        for k, v in schema.items():
            res[k] = dereference_schema(v, root_doc, seen)
        return res

    elif isinstance(schema, list):
        return [dereference_schema(item, root_doc, seen) for item in schema]

    return schema


def extract_required_fields(schema: Any) -> List[str]:
    """Extracts required fields from a schema, inspecting top-level and allOf."""
    fields: Set[str] = set()
    if isinstance(schema, dict):
        req = schema.get("required")
        if isinstance(req, list):
            for r in req:
                if isinstance(r, str):
                    fields.add(r)
        all_of = schema.get("allOf")
        if isinstance(all_of, list):
            for sub in all_of:
                fields.update(extract_required_fields(sub))
    return sorted(list(fields))


def make_id(method: str, path: str, operation_id: Optional[str] = None) -> str:
    if operation_id:
        return sanitize_id(operation_id)
    clean_path = path.strip("/").replace("/", "-").replace("{", "").replace("}", "")
    if not clean_path:
        clean_path = "root"
    method_name = method.lower()
    if method_name == "post":
        prefix = "create"
    elif method_name == "get":
        prefix = "get"
    elif method_name == "delete":
        prefix = "delete"
    elif method_name == "put":
        prefix = "update"
    elif method_name == "patch":
        prefix = "patch"
    else:
        prefix = method_name
    return sanitize_id(f"{prefix}-{clean_path}")


def make_name(method: str, path: str, summary: Optional[str] = None) -> str:
    if summary:
        return summary.strip()
    clean_path = path.strip("/")
    return f"{method.upper()} /{clean_path}"


def get_status_codes(responses: Dict[Any, Any], default: int = 200) -> List[int]:
    codes: List[int] = []
    for code_str in responses.keys():
        try:
            code = int(code_str)
            codes.append(code)
        except (ValueError, TypeError):
            continue
    codes = sorted(list(set(codes)))
    return codes if codes else [default]


def get_success_status_codes(responses: Dict[Any, Any], default: int = 200) -> List[int]:
    all_codes = get_status_codes(responses, default)
    success = [c for c in all_codes if 200 <= c < 300]
    return success if success else ([all_codes[0]] if all_codes else [default])


def extract_schema_info(
    response_obj: Dict[str, Any],
    root_doc: Dict[str, Any],
) -> Tuple[Optional[str], Optional[List[str]], Optional[Dict[str, Any]]]:
    """Extract contentType, requiredFields, and expanded bodySchema from OpenAPI response."""
    # If the response itself is a $ref, resolve it
    if "$ref" in response_obj:
        target = resolve_ref(response_obj["$ref"], root_doc)
        if isinstance(target, dict):
            response_obj = target

    content = response_obj.get("content")
    if not isinstance(content, dict):
        return None, None, None

    for content_type, details in content.items():
        if "json" in content_type.lower() and isinstance(details, dict):
            raw_schema = details.get("schema")
            if isinstance(raw_schema, (dict, list)):
                derefed_schema = dereference_schema(raw_schema, root_doc)
                req_fields = extract_required_fields(derefed_schema)
                return content_type, (req_fields if req_fields else None), derefed_schema
            return content_type, None, None

    first_type = next(iter(content.keys()), None)
    return first_type, None, None


def extract_request_body_info(
    request_body: Dict[str, Any],
    root_doc: Dict[str, Any],
) -> Tuple[Optional[Dict[str, str]], Optional[Any]]:
    """Extract headers and sample body from OpenAPI requestBody, dereferencing schemas."""
    if "$ref" in request_body:
        target = resolve_ref(request_body["$ref"], root_doc)
        if isinstance(target, dict):
            request_body = target

    content = request_body.get("content")
    if not isinstance(content, dict):
        return None, None

    for content_type, details in content.items():
        if isinstance(details, dict):
            headers = {"Content-Type": content_type}
            if "example" in details:
                return headers, details["example"]
            if "examples" in details and isinstance(details["examples"], dict):
                first_ex = next(iter(details["examples"].values()), None)
                if isinstance(first_ex, dict) and "value" in first_ex:
                    return headers, first_ex["value"]

            schema = details.get("schema")
            if isinstance(schema, (dict, list)):
                derefed_schema = dereference_schema(schema, root_doc)
                if isinstance(derefed_schema, dict) and "example" in derefed_schema:
                    return headers, derefed_schema["example"]
            return headers, None

    return None, None


def determine_role(path: str, operation: Dict[str, Any], global_security: Any, default_role: str) -> str:
    norm_path = path.lower()
    if norm_path == "/health" or norm_path.startswith("/health/") or norm_path == "/ping":
        return "public"
    return default_role


def find_parent_collection_path(item_path: str, known_paths: Set[str]) -> Optional[Tuple[str, str]]:
    """
    If item_path is like /api/tasks/{taskId}, checks if /api/tasks is in known_paths.
    Returns (parent_path, param_name) or None.
    """
    parts = item_path.rstrip("/").split("/")
    if parts and PATH_PARAM_RE.fullmatch(parts[-1]):
        param_name = PATH_PARAM_RE.fullmatch(parts[-1]).group(1)
        parent_path = "/".join(parts[:-1])
        if parent_path in known_paths:
            return parent_path, param_name
    return None


def generate_data_api(
    spec: Dict[str, Any],
    openapi_rel_path: str,
    solution_name: Optional[str] = None,
    team_id: Optional[str] = None,
    base_url: Optional[str] = None,
    default_role: str = "user",
    timeout_ms: int = 5000,
) -> Dict[str, Any]:
    info = spec.get("info") or {}
    title = info.get("title") or "API Service"
    final_solution_name = solution_name or title
    final_team_id = team_id or "test-team"

    # Base URL determination
    servers = spec.get("servers") or []
    detected_base_url = None
    if isinstance(servers, list) and servers:
        first_url = (servers[0] or {}).get("url")
        if isinstance(first_url, str) and first_url.startswith("https://"):
            detected_base_url = first_url

    if base_url:
        final_base_url = base_url
    elif detected_base_url:
        final_base_url = detected_base_url
    else:
        final_base_url = "https://api.example.org"

    paths_dict: Dict[str, Any] = spec.get("paths") or {}
    global_security = spec.get("security")

    all_path_strings = set(paths_dict.keys())

    # Map collections and parent-child relationships
    collections_with_post: Dict[str, str] = {}

    for path, path_item in paths_dict.items():
        if not isinstance(path_item, dict):
            continue
        if "post" in path_item:
            op = path_item["post"]
            post_id = make_id("post", path, op.get("operationId"))
            collections_with_post[path] = post_id

    checks: List[Dict[str, Any]] = []
    cleanup_steps: List[Dict[str, Any]] = []
    produced_vars: Set[str] = set()

    def sort_key(p: str) -> Tuple[int, str]:
        if "health" in p.lower() or "ping" in p.lower():
            return 0, p
        if not PATH_PARAM_RE.search(p):
            return 1, p
        return 2, p

    sorted_paths = sorted(paths_dict.keys(), key=sort_key)

    for path in sorted_paths:
        path_item = paths_dict[path]
        if not isinstance(path_item, dict):
            continue

        for method in ["get", "post", "put", "patch", "delete"]:
            if method not in path_item:
                continue

            op = path_item[method]
            if not isinstance(op, dict):
                continue

            op_id = op.get("operationId")
            summary = op.get("summary")
            responses = op.get("responses") or {}
            method_upper = method.upper()

            role = determine_role(path, op, global_security, default_role)

            if method == "delete":
                cleanup_id = make_id("delete", path, op_id)

                cleanup_step: Dict[str, Any] = {
                    "id": cleanup_id,
                    "method": "DELETE",
                    "path": path,
                    "role": role,
                }

                path_placeholders = PATH_PARAM_RE.findall(path)
                req_obj: Dict[str, Any] = {}
                if path_placeholders:
                    path_map: Dict[str, Any] = {}
                    for param in path_placeholders:
                        path_map[param] = f"${{{param}}}"
                    req_obj["path"] = path_map

                req_body = op.get("requestBody")
                if isinstance(req_body, dict):
                    headers, sample_body = extract_request_body_info(req_body, spec)
                    if headers:
                        req_obj["headers"] = headers
                    if sample_body is not None:
                        req_obj["body"] = sample_body

                if req_obj:
                    cleanup_step["request"] = req_obj

                all_codes = get_status_codes(responses, 204)
                if 404 not in all_codes:
                    all_codes.append(404)
                cleanup_step["expected"] = {"statusCodes": sorted(list(set(all_codes)))}
                cleanup_step["timeoutMs"] = timeout_ms

                cleanup_steps.append(cleanup_step)
                continue

            # Regular check
            parent_info = find_parent_collection_path(path, all_path_strings)
            check_id = make_id(method, path, op_id)
            name = make_name(method, path, summary)

            check: Dict[str, Any] = {
                "id": check_id,
                "name": name,
                "method": method_upper,
                "path": path,
                "role": role,
            }

            path_placeholders = PATH_PARAM_RE.findall(path)
            request_obj: Dict[str, Any] = {}

            if parent_info:
                parent_path, param_name = parent_info
                post_check_id = collections_with_post.get(parent_path)
                if post_check_id:
                    check["dependsOn"] = [post_check_id]

            if path_placeholders:
                path_map: Dict[str, Any] = {}
                for param in path_placeholders:
                    if param in produced_vars:
                        path_map[param] = f"${{{param}}}"
                    elif parent_info and param == parent_info[1]:
                        path_map[param] = f"${{{param}}}"
                    else:
                        path_map[param] = "sample-id"
                request_obj["path"] = path_map

            req_body = op.get("requestBody")
            if isinstance(req_body, dict):
                headers, sample_body = extract_request_body_info(req_body, spec)
                if headers:
                    request_obj["headers"] = headers
                if sample_body is not None:
                    request_obj["body"] = sample_body

            if request_obj:
                check["request"] = request_obj

            success_codes = get_success_status_codes(responses, 201 if method == "post" else 200)
            expected_obj: Dict[str, Any] = {"statusCodes": success_codes}

            first_success_resp = None
            for sc in success_codes:
                if str(sc) in responses:
                    first_success_resp = responses[str(sc)]
                    break

            if isinstance(first_success_resp, dict):
                c_type, req_fields, b_schema = extract_schema_info(first_success_resp, spec)
                if c_type:
                    expected_obj["contentType"] = c_type
                if req_fields:
                    expected_obj["requiredFields"] = req_fields
                if b_schema:
                    expected_obj["bodySchema"] = b_schema

            check["expected"] = expected_obj
            check["timeoutMs"] = timeout_ms

            if method_upper in {"POST", "PUT", "PATCH", "DELETE"}:
                check["repeatable"] = False
            else:
                check["repeatable"] = True

            if method == "post":
                for other_p in sorted_paths:
                    p_info = find_parent_collection_path(other_p, all_path_strings)
                    if p_info and p_info[0] == path:
                        var_name = p_info[1]
                        check["extract"] = {var_name: "$.id"}
                        produced_vars.add(var_name)
                        break

            checks.append(check)

    doc: Dict[str, Any] = {
        "schemaVersion": "1.0",
        "solution": {
            "name": final_solution_name,
            "teamId": final_team_id,
        },
        "api": {
            "baseUrl": final_base_url,
            "openapi": openapi_rel_path,
            "defaultHeaders": {
                "Accept": "application/json"
            },
        },
        "checks": checks,
    }

    if cleanup_steps:
        doc["cleanup"] = cleanup_steps

    return doc


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Autogenerate DATA-API.yaml from OpenAPI 3.0/3.1 specification."
    )
    parser.add_argument(
        "--openapi",
        "-i",
        default="openapi.yaml",
        help="Path to openapi.yaml or openapi.json (default: openapi.yaml)",
    )
    parser.add_argument(
        "--output",
        "-o",
        default="DATA-API.yaml",
        help="Path for generated DATA-API.yaml (default: DATA-API.yaml)",
    )
    parser.add_argument(
        "--solution-name",
        help="Solution name (defaults to OpenAPI info.title)",
    )
    parser.add_argument(
        "--team-id",
        default="test-team",
        help="Team ID (default: test-team)",
    )
    parser.add_argument(
        "--base-url",
        help="Base URL for testing (must be https://, defaults to OpenAPI servers[0].url)",
    )
    parser.add_argument(
        "--default-role",
        default="user",
        help="Default role for non-public checks (default: user)",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=5000,
        help="Default timeout in milliseconds (default: 5000)",
    )

    args = parser.parse_args()

    openapi_path = Path(args.openapi).resolve()
    if not openapi_path.exists():
        print(f"Error: OpenAPI file not found: {openapi_path}", file=sys.stderr)
        return 1

    try:
        content = openapi_path.read_text(encoding="utf-8")
        if openapi_path.suffix.lower() == ".json":
            import json
            spec = json.loads(content)
        else:
            spec = yaml.safe_load(content)
    except Exception as exc:
        print(f"Error reading OpenAPI file: {exc}", file=sys.stderr)
        return 1

    output_path = Path(args.output).resolve()
    try:
        rel_openapi = os.path.relpath(openapi_path, output_path.parent)
        if not rel_openapi.startswith("."):
            rel_openapi = f"./{rel_openapi}"
    except ValueError:
        rel_openapi = str(openapi_path)

    data_api_doc = generate_data_api(
        spec=spec,
        openapi_rel_path=rel_openapi,
        solution_name=args.solution_name,
        team_id=args.team_id,
        base_url=args.base_url,
        default_role=args.default_role,
        timeout_ms=args.timeout,
    )

    class CustomDumper(yaml.SafeDumper):
        pass

    def str_representer(dumper: yaml.SafeDumper, data: str) -> yaml.ScalarNode:
        if "\n" in data:
            return dumper.represent_scalar("tag:yaml.org,2002:str", data, style="|")
        return dumper.represent_scalar("tag:yaml.org,2002:str", data)

    CustomDumper.add_representer(str, str_representer)

    yaml_text = yaml.dump(
        data_api_doc,
        Dumper=CustomDumper,
        sort_keys=False,
        allow_unicode=True,
        indent=2,
    )

    header = "# DATA-API 1.0\n# Autogenerated from OpenAPI specification\n\n"
    output_path.write_text(header + yaml_text, encoding="utf-8")
    print(f"Successfully generated {output_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
