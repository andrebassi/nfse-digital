from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..models.environment import Environment
from ..types import UNSET, Unset

T = TypeVar("T", bound="Account")


@_attrs_define
class Account:
    """
    Attributes:
        legal_name (str | Unset):
        cnpj (str | Unset):
        environment (Environment | Unset):
        series (str | Unset):
        next_dps_number (int | Unset):
    """

    legal_name: str | Unset = UNSET
    cnpj: str | Unset = UNSET
    environment: Environment | Unset = UNSET
    series: str | Unset = UNSET
    next_dps_number: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)

    def to_dict(self) -> dict[str, Any]:
        legal_name = self.legal_name

        cnpj = self.cnpj

        environment: str | Unset = UNSET
        if not isinstance(self.environment, Unset):
            environment = self.environment.value

        series = self.series

        next_dps_number = self.next_dps_number

        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({})
        if legal_name is not UNSET:
            field_dict["legal_name"] = legal_name
        if cnpj is not UNSET:
            field_dict["cnpj"] = cnpj
        if environment is not UNSET:
            field_dict["environment"] = environment
        if series is not UNSET:
            field_dict["series"] = series
        if next_dps_number is not UNSET:
            field_dict["next_dps_number"] = next_dps_number

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        legal_name = d.pop("legal_name", UNSET)

        cnpj = d.pop("cnpj", UNSET)

        _environment = d.pop("environment", UNSET)
        environment: Environment | Unset
        if isinstance(_environment, Unset):
            environment = UNSET
        else:
            environment = Environment(_environment)

        series = d.pop("series", UNSET)

        next_dps_number = d.pop("next_dps_number", UNSET)

        account = cls(
            legal_name=legal_name,
            cnpj=cnpj,
            environment=environment,
            series=series,
            next_dps_number=next_dps_number,
        )

        account.additional_properties = d
        return account

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
