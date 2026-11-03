from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..models.cancel_request_reason import CancelRequestReason
from ..types import UNSET, Unset

T = TypeVar("T", bound="CancelRequest")


@_attrs_define
class CancelRequest:
    """
    Attributes:
        reason (CancelRequestReason):
        description (str): justificativa
        confirm_production (bool | Unset): obrigatório true para nota de produção Default: False.
    """

    reason: CancelRequestReason
    description: str
    confirm_production: bool | Unset = False

    def to_dict(self) -> dict[str, Any]:
        reason = self.reason.value

        description = self.description

        confirm_production = self.confirm_production

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "reason": reason,
                "description": description,
            }
        )
        if confirm_production is not UNSET:
            field_dict["confirm_production"] = confirm_production

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        reason = CancelRequestReason(d.pop("reason"))

        description = d.pop("description")

        confirm_production = d.pop("confirm_production", UNSET)

        cancel_request = cls(
            reason=reason,
            description=description,
            confirm_production=confirm_production,
        )

        return cancel_request
