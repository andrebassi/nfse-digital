from http import HTTPStatus
from typing import Any, cast
from urllib.parse import quote

import httpx

from ... import errors
from ...client import AuthenticatedClient, Client
from ...models.emit_request import EmitRequest
from ...models.error import Error
from ...models.invoice import Invoice
from ...types import UNSET, Response


def _get_kwargs(
    *,
    body: EmitRequest,
) -> dict[str, Any]:
    headers: dict[str, Any] = {}

    _kwargs: dict[str, Any] = {
        "method": "post",
        "url": "/invoices",
    }

    _kwargs["json"] = body.to_dict()

    headers["Content-Type"] = "application/json"

    _kwargs["headers"] = headers
    return _kwargs


def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Error | Invoice | None:
    if response.status_code == 200:
        response_200 = Invoice.from_dict(response.json())

        return response_200

    if response.status_code == 201:
        response_201 = Invoice.from_dict(response.json())

        return response_201

    if response.status_code == 202:
        response_202 = Invoice.from_dict(response.json())

        return response_202

    if response.status_code == 400:
        response_400 = Error.from_dict(response.json())

        return response_400

    if response.status_code == 401:
        response_401 = Error.from_dict(response.json())

        return response_401

    if response.status_code == 409:
        response_409 = Error.from_dict(response.json())

        return response_409

    if response.status_code == 422:
        response_422 = Error.from_dict(response.json())

        return response_422

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[Error | Invoice]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient | Client,
    body: EmitRequest,
) -> Response[Error | Invoice]:
    """Emitir nota

     Emite pela mesma regra do painel. Campos vazios vêm do modelo: `amount` usa o valor padrão,
    `competence` usa hoje e `description` usa a do modelo com `{mes}` e `{ano}` resolvidos.

    Repetir a mesma `idempotency_key` devolve a nota já criada (200), sem emitir outra.

    Args:
        body (EmitRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[Error | Invoice]
    """

    kwargs = _get_kwargs(
        body=body,
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)


def sync(
    *,
    client: AuthenticatedClient | Client,
    body: EmitRequest,
) -> Error | Invoice | None:
    """Emitir nota

     Emite pela mesma regra do painel. Campos vazios vêm do modelo: `amount` usa o valor padrão,
    `competence` usa hoje e `description` usa a do modelo com `{mes}` e `{ano}` resolvidos.

    Repetir a mesma `idempotency_key` devolve a nota já criada (200), sem emitir outra.

    Args:
        body (EmitRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Error | Invoice
    """

    return sync_detailed(
        client=client,
        body=body,
    ).parsed


async def asyncio_detailed(
    *,
    client: AuthenticatedClient | Client,
    body: EmitRequest,
) -> Response[Error | Invoice]:
    """Emitir nota

     Emite pela mesma regra do painel. Campos vazios vêm do modelo: `amount` usa o valor padrão,
    `competence` usa hoje e `description` usa a do modelo com `{mes}` e `{ano}` resolvidos.

    Repetir a mesma `idempotency_key` devolve a nota já criada (200), sem emitir outra.

    Args:
        body (EmitRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[Error | Invoice]
    """

    kwargs = _get_kwargs(
        body=body,
    )

    response = await client.get_async_httpx_client().request(**kwargs)

    return _build_response(client=client, response=response)


async def asyncio(
    *,
    client: AuthenticatedClient | Client,
    body: EmitRequest,
) -> Error | Invoice | None:
    """Emitir nota

     Emite pela mesma regra do painel. Campos vazios vêm do modelo: `amount` usa o valor padrão,
    `competence` usa hoje e `description` usa a do modelo com `{mes}` e `{ano}` resolvidos.

    Repetir a mesma `idempotency_key` devolve a nota já criada (200), sem emitir outra.

    Args:
        body (EmitRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Error | Invoice
    """

    return (
        await asyncio_detailed(
            client=client,
            body=body,
        )
    ).parsed
