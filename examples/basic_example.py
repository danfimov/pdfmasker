import pathlib

from pdfmasker import Masker

masker = Masker()
with pathlib.Path("tests/fixtures/paystubs/adp_paystub_hermion_granger.pdf").open("rb") as file:
    output = masker.mask(file.read(), patterns=["HERMIONE GRANGER"])
    for entry in output.entries:
        print(entry)

with pathlib.Path("masked.pdf").open("wb") as output_file:
    output_file.write(output.document_content)
