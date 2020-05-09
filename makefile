pageturner :
	go build -o pageturner main.go

.PHONY : clean
clean :
	rm pageturner

.PHONY : install
install :
	cp page_turner_*.sh /usr/local/bin
	cp pageturner /usr/local/bin