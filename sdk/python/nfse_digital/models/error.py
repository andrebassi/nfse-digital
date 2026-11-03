from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, BinaryIO, Generator, TextIO, TypeVar, cast

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

if TYPE_CHECKING:
    from ..models.error_fields import ErrorFields
    from ..models.invoice import Invoice


T = TypeVar("T", bound="Error")


@_attrs_define
class Error:
    """
    Attributes:
        error (str):
        fields (ErrorFields | Unset):
        invoice (Invoice | Unset):
    """

    error: str
    fields: ErrorFields | Unset = UNSET
    invoice: Invoice | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)

    def to_dict(self) -> dict[str, Any]:
        from ..models.error_fields import ErrorFields
        from ..models.invoice import Invoice

        error = self.error

        fields: dict[str, Any] | Unset = UNSET
        if not isinstance(self.fields, Unset):
            fields = self.fields.to_dict()

        invoice: dict[str, Any] | Unset = UNSET
        if not isinstance(self.invoice, Unset):
            invoice = self.invoice.to_dict()

        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update(
            {
                "error": error,
            }
        )
        if fields is not UNSET:
            field_dict["fields"] = fields
        if invoice is not UNSET:
            field_dict["invoice"] = invoice

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.error_fields import ErrorFields
        from ..models.invoice import Invoice

        d = dict(src_dict)
        error = d.pop("error")

        _fields = d.pop("fields", UNSET)
        fields: ErrorFields | Unset
        if isinstance(_fields, Unset):
            fields = UNSET
        else:
            fields = ErrorFields.from_dict(_fields)

        _invoice = d.pop("invoice", UNSET)
        invoice: Invoice | Unset
        if isinstance(_invoice, Unset):
            invoice = UNSET
        else:
            invoice = Invoice.from_dict(_invoice)

        error = cls(
            error=error,
            fields=fields,
            invoice=invoice,
        )

        error.additional_properties = d
        return error

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
