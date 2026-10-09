# Biên dịch báo cáo và kết xuất sơ đồ
#   make            -> biên dịch output/pdf/bao-cao.pdf
#   make diagrams   -> kết xuất lại toàn bộ sơ đồ PlantUML thành PNG
#   make clean      -> xóa tệp trung gian
MAIN     ?= main
LATEXMK  ?= latexmk
PLANTUML ?= plantuml

.PHONY: all setup pdf diagrams watch clean distclean

all: pdf

setup:
	./setup.sh

pdf:
	./run.sh

diagrams:
	cd diagrams && $(PLANTUML) -tpng -o ../figures/diagrams c*.puml

watch:
	$(LATEXMK) -xelatex -outdir=build -pvc $(MAIN).tex

clean:
	$(LATEXMK) -outdir=build -c $(MAIN).tex

distclean:
	$(LATEXMK) -outdir=build -C $(MAIN).tex
