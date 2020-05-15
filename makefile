pageturner :
	go generate
	go build -o pageturner

.PHONY : clean
clean :
	rm pageturner

.PHONY : install
install :
	cp page_turner_*.sh /usr/local/bin
	cp pageturner /usr/local/bin

.PHONY : clean_obsolete
clean_obsolete :
	rm /usr/local/bin/page_turner_cleanup.sh