ifneq (,$(findstring synology,$(shell uname -a)))
	destination := /opt/usr/local/bin
else
	destination := /usr/local/bin
endif


pageturner :
	go generate
	go build -o pageturner

.PHONY : clean
clean :
	rm pageturner || true
	rm bindata.go || true

.PHONY : install
install :
	cp pageturner $(destination)

.PHONY : clean_obsolete
clean_obsolete :
	rm /usr/local/bin/page_turner_cleanup.sh || true
	rm /usr/local/bin/page_turner_convert.sh || true
	rm /usr/local/bin/page_turner_cover.sh || true
	rm /usr/local/bin/page_turner_merge.sh || true