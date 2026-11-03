from __future__ import annotations

import datetime
from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar, cast

from attrs import define as _attrs_define
from attrs import field as _attrs_field
from dateutil.parser import isoparse

from ..types import UNSET, Unset

T = TypeVar("T", bound="EmitRequest")


@_attrs_define
class EmitRequest:
    """
    Attributes:
        idempotency_key (str): id estável do seu sistema (ex.: o id da fatura); repetir nunca emite outra nota
        customer_id (int): GET /customers
        template_id (int): GET /templates
        amount (str | Unset): 1500.00 ou 1.500,00; vazio usa o valor do modelo
        competence (datetime.date | Unset): AAAA-MM-DD; vazio usa hoje
        description (str | Unset): vazio usa a do modelo, com {mes} e {ano} resolvidos
        confirm_production (bool | Unset): obrigatório true com o emitente em produção Default: False.
    """

    idempotency_key: str
    customer_id: int
    template_id: int
    amount: str | Unset = UNSET
    competence: datetime.date | Unset = UNSET
    description: str | Unset = UNSET
    confirm_production: bool | Unset = False

    def to_dict(self) -> dict[str, Any]:
        idempotency_key = self.idempotency_key

        customer_id = self.customer_id

        template_id = self.template_id

        amount = self.amount

        competence: str | Unset = UNSET
        if not isinstance(self.competence, Unset):
            competence = self.competence.isoformat()

        description = self.description

        confirm_production = self.confirm_production

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "idempotency_key": idempotency_key,
                "customer_id": customer_id,
                "template_id": template_id,
            }
        )
        if amount is not UNSET:
            field_dict["amount"] = amount
        if competence is not UNSET:
            field_dict["competence"] = competence
        if description is not UNSET:
            field_dict["description"] = description
        if confirm_production is not UNSET:
            field_dict["confirm_production"] = confirm_production

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        idempotency_key = d.pop("idempotency_key")

        customer_id = d.pop("customer_id")

        template_id = d.pop("template_id")

        amount = d.pop("amount", UNSET)

        _competence = d.pop("competence", UNSET)
        competence: datetime.date | Unset
        if isinstance(_competence, Unset):
            competence = UNSET
        else:
            competence = isoparse(_competence).date()

        description = d.pop("description", UNSET)

        confirm_production = d.pop("confirm_production", UNSET)

        emit_request = cls(
            idempotency_key=idempotency_key,
            customer_id=customer_id,
            template_id=template_id,
            amount=amount,
            competence=competence,
            description=description,
            confirm_production=confirm_production,
        )

        return emit_request
