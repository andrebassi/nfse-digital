from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

T = TypeVar("T", bound="Customer")


@_attrs_define
class Customer:
    """
    Attributes:
        id (int | Unset):
        name (str | Unset):
        document (str | Unset): CPF ou CNPJ, só dígitos
        email (str | Unset):
        city (str | Unset):
    """

    id: int | Unset = UNSET
    name: str | Unset = UNSET
    document: str | Unset = UNSET
    email: str | Unset = UNSET
    city: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)

    def to_dict(self) -> dict[str, Any]:
        id = self.id

        name = self.name

        document = self.document

        email = self.email

        city = self.city

        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({})
        if id is not UNSET:
            field_dict["id"] = id
        if name is not UNSET:
            field_dict["name"] = name
        if document is not UNSET:
            field_dict["document"] = document
        if email is not UNSET:
            field_dict["email"] = email
        if city is not UNSET:
            field_dict["city"] = city

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id", UNSET)

        name = d.pop("name", UNSET)

        document = d.pop("document", UNSET)

        email = d.pop("email", UNSET)

        city = d.pop("city", UNSET)

        customer = cls(
            id=id,
            name=name,
            document=document,
            email=email,
            city=city,
        )

        customer.additional_properties = d
        return customer

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
