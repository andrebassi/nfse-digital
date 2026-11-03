from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

T = TypeVar("T", bound="Template")


@_attrs_define
class Template:
    """
    Attributes:
        id (int | Unset):
        name (str | Unset):
        service_code (str | Unset): código de tributação nacional
        default_amount (str | Unset):  Example: 1500.00.
        description (str | Unset):
    """

    id: int | Unset = UNSET
    name: str | Unset = UNSET
    service_code: str | Unset = UNSET
    default_amount: str | Unset = UNSET
    description: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)

    def to_dict(self) -> dict[str, Any]:
        id = self.id

        name = self.name

        service_code = self.service_code

        default_amount = self.default_amount

        description = self.description

        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({})
        if id is not UNSET:
            field_dict["id"] = id
        if name is not UNSET:
            field_dict["name"] = name
        if service_code is not UNSET:
            field_dict["service_code"] = service_code
        if default_amount is not UNSET:
            field_dict["default_amount"] = default_amount
        if description is not UNSET:
            field_dict["description"] = description

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id", UNSET)

        name = d.pop("name", UNSET)

        service_code = d.pop("service_code", UNSET)

        default_amount = d.pop("default_amount", UNSET)

        description = d.pop("description", UNSET)

        template = cls(
            id=id,
            name=name,
            service_code=service_code,
            default_amount=default_amount,
            description=description,
        )

        template.additional_properties = d
        return template

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
