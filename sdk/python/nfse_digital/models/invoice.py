from __future__ import annotations

import datetime
from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar, cast

from attrs import define as _attrs_define
from attrs import field as _attrs_field
from dateutil.parser import isoparse

from ..models.environment import Environment
from ..models.invoice_status import InvoiceStatus
from ..types import UNSET, Unset

T = TypeVar("T", bound="Invoice")


@_attrs_define
class Invoice:
    """
    Attributes:
        id (int):  Example: 42.
        status (InvoiceStatus):
        status_label (str):  Example: autorizada.
        environment (Environment):
        dps_series (str):  Example: 900.
        dps_number (int):  Example: 1001.
        amount (str): ponto decimal, duas casas Example: 1500.00.
        customer_id (int):
        template_id (int):
        description (str):
        idempotency_key (str | Unset):  Example: fatura-2026-11-0042.
        nfse_number (str | Unset): número da NFS-e (autorizada)
        access_key (str | Unset): chave de acesso de 50 dígitos (autorizada)
        competence (datetime.date | Unset):  Example: 2026-11-01.
        issued_at (datetime.datetime | Unset):
        error_code (str | Unset): código da SEFIN (rejeitada) Example: E0039.
        message (str | Unset): erro da SEFIN traduzido, ou aviso de resposta pendente
    """

    id: int
    status: InvoiceStatus
    status_label: str
    environment: Environment
    dps_series: str
    dps_number: int
    amount: str
    customer_id: int
    template_id: int
    description: str
    idempotency_key: str | Unset = UNSET
    nfse_number: str | Unset = UNSET
    access_key: str | Unset = UNSET
    competence: datetime.date | Unset = UNSET
    issued_at: datetime.datetime | Unset = UNSET
    error_code: str | Unset = UNSET
    message: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)

    def to_dict(self) -> dict[str, Any]:
        id = self.id

        status = self.status.value

        status_label = self.status_label

        environment = self.environment.value

        dps_series = self.dps_series

        dps_number = self.dps_number

        amount = self.amount

        customer_id = self.customer_id

        template_id = self.template_id

        description = self.description

        idempotency_key = self.idempotency_key

        nfse_number = self.nfse_number

        access_key = self.access_key

        competence: str | Unset = UNSET
        if not isinstance(self.competence, Unset):
            competence = self.competence.isoformat()

        issued_at: str | Unset = UNSET
        if not isinstance(self.issued_at, Unset):
            issued_at = self.issued_at.isoformat()

        error_code = self.error_code

        message = self.message

        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update(
            {
                "id": id,
                "status": status,
                "status_label": status_label,
                "environment": environment,
                "dps_series": dps_series,
                "dps_number": dps_number,
                "amount": amount,
                "customer_id": customer_id,
                "template_id": template_id,
                "description": description,
            }
        )
        if idempotency_key is not UNSET:
            field_dict["idempotency_key"] = idempotency_key
        if nfse_number is not UNSET:
            field_dict["nfse_number"] = nfse_number
        if access_key is not UNSET:
            field_dict["access_key"] = access_key
        if competence is not UNSET:
            field_dict["competence"] = competence
        if issued_at is not UNSET:
            field_dict["issued_at"] = issued_at
        if error_code is not UNSET:
            field_dict["error_code"] = error_code
        if message is not UNSET:
            field_dict["message"] = message

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id")

        status = InvoiceStatus(d.pop("status"))

        status_label = d.pop("status_label")

        environment = Environment(d.pop("environment"))

        dps_series = d.pop("dps_series")

        dps_number = d.pop("dps_number")

        amount = d.pop("amount")

        customer_id = d.pop("customer_id")

        template_id = d.pop("template_id")

        description = d.pop("description")

        idempotency_key = d.pop("idempotency_key", UNSET)

        nfse_number = d.pop("nfse_number", UNSET)

        access_key = d.pop("access_key", UNSET)

        _competence = d.pop("competence", UNSET)
        competence: datetime.date | Unset
        if isinstance(_competence, Unset):
            competence = UNSET
        else:
            competence = isoparse(_competence).date()

        _issued_at = d.pop("issued_at", UNSET)
        issued_at: datetime.datetime | Unset
        if isinstance(_issued_at, Unset):
            issued_at = UNSET
        else:
            issued_at = isoparse(_issued_at)

        error_code = d.pop("error_code", UNSET)

        message = d.pop("message", UNSET)

        invoice = cls(
            id=id,
            status=status,
            status_label=status_label,
            environment=environment,
            dps_series=dps_series,
            dps_number=dps_number,
            amount=amount,
            customer_id=customer_id,
            template_id=template_id,
            description=description,
            idempotency_key=idempotency_key,
            nfse_number=nfse_number,
            access_key=access_key,
            competence=competence,
            issued_at=issued_at,
            error_code=error_code,
            message=message,
        )

        invoice.additional_properties = d
        return invoice

    @property
    def additional_keys(self) -> list[str]:
        return list(self.additional_properties.keys())

    def __getitem__(self, key: str) -> Any:
        return self.additional_properties[key]

    def __setitem__(self, key: str, value: Any) -> None:
        self.additional_properties[key] = value

    def __delitem__(self, key: str) -> None:
        del self.additional_properties[key]

    def __contains__(self, key: str) -> bool:
        return key in self.additional_properties
