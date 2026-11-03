from http import HTTPStatus
from typing import Any, cast
from urllib.parse import quote

import httpx

from ... import errors
from ...client import AuthenticatedClient, Client
from ...models.error import Error
from ...models.invoice import Invoice
from ...types import UNSET, Response


def _get_kwargs(
    *,
    idempotency_key: str,
) -> dict[str, Any]:

    params: dict[str, Any] = {}

    params["idempotency_key"] = idempotency_key

    params = {k: v for k, v in params.items() if v is not UNSET and v is not None}

    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/invoices/by-key",
        "params": params,
    }

    return _kwargs


def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Error | Invoice | None:
    if response.status_code == 200:
        response_200 = Invoice.from_dict(response.json())

        return response_200

    if response.status_code == 401:
        response_401 = Error.from_dict(response.json())

        return response_401

    if response.status_code == 404:
        response_404 = Error.from_dict(response.json())

        return response_404

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
    idempotency_key: str,
) -> Response[Error | Invoice]:
    """Consultar nota pela idempotency_key

     O id da fatura no seu sistema basta: você não precisa guardar o id do nfse-digital.

    Args:
        idempotency_key (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[Error | Invoice]
    """

    kwargs = _get_kwargs(
        idempotency_key=idempotency_key,
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)


def sync(
    *,
    client: AuthenticatedClient | Client,
    idempotency_key: str,
) -> Error | Invoice | None:
    """Consultar nota pela idempotency_key

     O id da fatura no seu sistema basta: você não precisa guardar o id do nfse-digital.

    Args:
        idempotency_key (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Error | Invoice
    """

    return sync_detailed(
        client=client,
        idempotency_key=idempotency_key,
    ).parsed


async def asyncio_detailed(
    *,
    client: AuthenticatedClient | Client,
    idempotency_key: str,
) -> Response[Error | Invoice]:
    """Consultar nota pela idempotency_key

     O id da fatura no seu sistema basta: você não precisa guardar o id do nfse-digital.

    Args:
        idempotency_key (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[Error | Invoice]
    """

    kwargs = _get_kwargs(
        idempotency_key=idempotency_key,
    )

    response = await client.get_async_httpx_client().request(**kwargs)

    return _build_response(client=client, response=response)


async def asyncio(
    *,
    client: AuthenticatedClient | Client,
    idempotency_key: str,
) -> Error | Invoice | None:
    """Consultar nota pela idempotency_key

     O id da fatura no seu sistema basta: você não precisa guardar o id do nfse-digital.

    Args:
        idempotency_key (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Error | Invoice
    """

    return (
        await asyncio_detailed(
            client=client,
            idempotency_key=idempotency_key,
        )
    ).parsed
