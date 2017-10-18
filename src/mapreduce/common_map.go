package mapreduce

import (
	"encoding/json"
	"hash/fnv"
	"log"
	"os"
)

// doMap manages one map task: it reads one of the input files
// (inFile), calls the user-defined map function (mapF) for that file's
// contents, and partitions the output into nReduce intermediate files.
func doMap(
	jobName string, // the name of the MapReduce job
	mapTaskNumber int, // which map task this is
	inFile string,
	nReduce int, // the number of reduce task that will be run ("R" in the paper)
	mapF func(file string, contents string) []KeyValue,
) {
	//
	// You will need to write this function.
	//
	// The intermediate output of a map task is stored as multiple
	// files, one per destination reduce task. The file name includes
	// both the map task number and the reduce task number. Use the
	// filename generated by reduceName(jobName, mapTaskNumber, r) as
	// the intermediate file for reduce task r. Call ihash() (see below)
	// on each key, mod nReduce, to pick r for a key/value pair.
	//
	// mapF() is the map function provided by the application. The first
	// argument should be the input file name, though the map function
	// typically ignores it. The second argument should be the entire
	// input file contents. mapF() returns a slice containing the
	// key/value pairs for reduce; see common.go for the definition of
	// KeyValue.
	//
	// Look at Go's ioutil and os packages for functions to read
	// and write files.
	//
	// Coming up with a scheme for how to format the key/value pairs on
	// disk can be tricky, especially when taking into account that both
	// keys and values could contain newlines, quotes, and any other
	// character you can think of.
	//
	// One format often used for serializing data to a byte stream that the
	// other end can correctly reconstruct is JSON. You are not required to
	// use JSON, but as the output of the reduce tasks *must* be JSON,
	// familiarizing yourself with it here may prove useful. You can write
	// out a data structure as a JSON string to a file using the commented
	// code below. The corresponding decoding functions can be found in
	// common_reduce.go.
	//
	//   enc := json.NewEncoder(file)
	//   for _, kv := ... {
	//     err := enc.Encode(&kv)
	//
	// Remember to close the file after you have written all the values!
	file, err := os.Open(inFile) // For read access.
	//fmt.Println(inFile)
	if err != nil {
		log.Fatal("doMap failed to open input file: ", inFile, " error: ", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		log.Fatal("get input file info failed: ", file, " error: ", err)
	}

	data := make([]byte, fi.Size())
	_, err = file.Read(data)
	if err != nil {
		log.Fatal("read inpufile failed: ", inFile, " error: ", err)
	}

	kv := mapF(fi.Name(), string(data))

	rFiles := make([]*os.File, nReduce)
	rEncodes := make([]*json.Encoder, nReduce)
	for i := 0; i < nReduce; i++ { //创建rReduce个中间文件供reduce步骤读取
		rFileName := reduceName(jobName, mapTaskNumber, i)
		rFile, err := os.Create(rFileName)
		if err != nil {
			log.Fatal("create middle file failed: ", err)
		}
		defer rFile.Close()
		rFiles[i] = rFile
		defer rFiles[i].Close()
		rEncodes[i] = json.NewEncoder(rFiles[i])
	}

	for _, v := range kv { //遍历经过mapF函数处理得到的中间键值对，分别写入对应的中间文件
		n := ihash(v.Key) % int(nReduce)
		err = rEncodes[n].Encode(v) //这里编码进去，在文件中是一行一行的json键值对，而且换行符是标准的LF，在windows下的时候要注意这个问题，windows下的换行符是CRLF，如果要进行文件对比的话注意区分
		if err != nil {
			log.Fatal("encode kv failed: ", err)
		}
	}

}

func ihash(s string) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32() & 0x7fffffff)
}
