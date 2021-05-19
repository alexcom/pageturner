pageturner :
	go generate
	go build -o pageturner

.PHONY : clean
clean :
	rm pageturner || true

.PHONY : install
install :
	cp pageturner /usr/local/bin
